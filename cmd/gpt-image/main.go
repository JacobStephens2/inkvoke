// Command gpt-image is a CLI for OpenAI's image models (gpt-image-2 by default):
// generate from a prompt, edit existing photos, or run a JSON batch.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const version = "0.2.1"

var (
	qualities = map[string]bool{"low": true, "medium": true, "high": true, "auto": true}
	sizes     = map[string]bool{
		"auto": true, "1024x1024": true, "1536x1024": true, "1024x1536": true,
	}
	formats = map[string]bool{"png": true, "jpeg": true, "webp": true}
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		printRootHelp(os.Stderr)
		return 2
	}
	switch args[0] {
	case "-h", "--help", "help":
		printRootHelp(os.Stdout)
		return 0
	case "-v", "--version", "version":
		fmt.Printf("gpt-image %s\n", version)
		return 0
	case "generate":
		return cmdGenerate(args[1:])
	case "edit":
		return cmdEdit(args[1:])
	case "batch":
		return cmdBatch(args[1:])
	case "hair-color":
		return cmdHairColor(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", args[0])
		printRootHelp(os.Stderr)
		return 2
	}
}

func printRootHelp(w io.Writer) {
	fmt.Fprint(w, `gpt-image - generate and edit images with OpenAI image models

Usage:
  gpt-image <command> [flags]

Commands:
  generate    Generate an image from a prompt
  edit        Edit one or more input images with a prompt
  batch       Generate or edit many images from a JSON manifest
  hair-color  Convenience edit for hair color (legacy Arena path)
  version     Print version

Global:
  -h, --help     Show help
  -v, --version  Show version

Environment:
  OPENAI_API_KEY   API key (or pass --api-key-file)

Examples:
  gpt-image generate "a lighthouse in a storm, gouache" --output lighthouse.png --quality high --size 1536x1024
  gpt-image edit photo.jpg --prompt "make the sky golden hour" --output golden.png
  gpt-image batch manifest.json --output-dir outputs --workers 3 --skip-existing
`)
}

type commonFlags struct {
	model        string
	quality      string
	size         string
	outputFormat string
	apiKeyFile   string
	noCost       bool
	quiet        bool
	timeoutSec   int
}

func addCommonFlags(fs *flag.FlagSet, c *commonFlags) {
	// Pre-set fields on c become flag defaults (hair-color uses quality medium).
	modelDef, qualityDef, sizeDef, formatDef := c.model, c.quality, c.size, c.outputFormat
	if modelDef == "" {
		modelDef = "gpt-image-2"
	}
	if qualityDef == "" {
		qualityDef = "auto"
	}
	if sizeDef == "" {
		sizeDef = "auto"
	}
	if formatDef == "" {
		formatDef = "png"
	}
	timeoutDef := c.timeoutSec
	if timeoutDef <= 0 {
		timeoutDef = int(defaultTimeout / time.Second)
	}
	fs.StringVar(&c.model, "model", modelDef, "OpenAI image model")
	fs.StringVar(&c.quality, "quality", qualityDef, "Quality: low, medium, high, auto")
	fs.StringVar(&c.size, "size", sizeDef, "Size: auto, 1024x1024, 1536x1024, 1024x1536, or WIDTHxHEIGHT for gpt-image-2")
	fs.StringVar(&c.outputFormat, "output-format", formatDef, "Output format: png, jpeg, webp")
	fs.StringVar(&c.apiKeyFile, "api-key-file", "", "File containing the OpenAI API key (fallback: OPENAI_API_KEY)")
	fs.BoolVar(&c.noCost, "no-cost", false, "Do not print estimated USD cost / token usage")
	fs.BoolVar(&c.quiet, "quiet", false, "Suppress progress heartbeats on stderr")
	fs.IntVar(&c.timeoutSec, "timeout", timeoutDef, "HTTP timeout in seconds per API attempt (high quality often needs 180+)")
}

func (c *commonFlags) validate() error {
	if !qualities[c.quality] {
		return fmt.Errorf("invalid --quality %q (want low|medium|high|auto)", c.quality)
	}
	if !formats[c.outputFormat] {
		return fmt.Errorf("invalid --output-format %q (want png|jpeg|webp)", c.outputFormat)
	}
	// Allow documented presets or free-form WIDTHxHEIGHT (gpt-image-2).
	if !sizes[c.size] {
		var w, h int
		if _, err := fmt.Sscanf(c.size, "%dx%d", &w, &h); err != nil || w < 1 || h < 1 {
			return fmt.Errorf("invalid --size %q", c.size)
		}
	}
	if c.timeoutSec < 30 {
		return fmt.Errorf("invalid --timeout %d (want >= 30 seconds)", c.timeoutSec)
	}
	return nil
}

func (c *commonFlags) newClient() (*Client, error) {
	key, err := resolveAPIKey(c.apiKeyFile)
	if err != nil {
		return nil, err
	}
	client := NewClient(key, time.Duration(c.timeoutSec)*time.Second)
	client.SetQuiet(c.quiet)
	return client, nil
}

func (c *commonFlags) options() RequestOptions {
	return RequestOptions{
		Model:        c.model,
		Quality:      c.quality,
		Size:         c.size,
		OutputFormat: c.outputFormat,
	}
}

// parseFlags allows flags before or after positional args (Python-argparse style).
func parseFlags(fs *flag.FlagSet, args []string) error {
	return fs.Parse(reorderFlagArgs(fs, args))
}

func boolFlagNames(fs *flag.FlagSet) map[string]bool {
	out := make(map[string]bool)
	fs.VisitAll(func(f *flag.Flag) {
		type boolFlag interface{ IsBoolFlag() bool }
		if bf, ok := f.Value.(boolFlag); ok && bf.IsBoolFlag() {
			out[f.Name] = true
		}
	})
	return out
}

func reorderFlagArgs(fs *flag.FlagSet, args []string) []string {
	isBool := boolFlagNames(fs)
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			pos = append(pos, a)
			continue
		}
		// -name=value or --name=value
		name := strings.TrimLeft(a, "-")
		if eq := strings.IndexByte(name, '='); eq >= 0 {
			name = name[:eq]
			flags = append(flags, a)
			continue
		}
		flags = append(flags, a)
		if isBool[name] {
			continue
		}
		// value flag: consume next token if present
		if i+1 < len(args) && (!strings.HasPrefix(args[i+1], "-") || args[i+1] == "-") {
			i++
			flags = append(flags, args[i])
		}
	}
	return append(flags, pos...)
}

func resolveAPIKey(path string) (string, error) {
	if path != "" {
		b, err := os.ReadFile(expandHome(path))
		if err != nil {
			return "", fmt.Errorf("read --api-key-file: %w", err)
		}
		key := strings.TrimSpace(string(b))
		if key == "" {
			return "", errors.New("API key file is empty")
		}
		return key, nil
	}
	key := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if key == "" {
		return "", errors.New("missing API key: set OPENAI_API_KEY or pass --api-key-file /path/to/key.txt")
	}
	return key, nil
}

func expandHome(p string) string {
	if p == "" {
		return p
	}
	if p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return p
	}
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

func cmdGenerate(args []string) int {
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var common commonFlags
	var output string
	addCommonFlags(fs, &common)
	fs.StringVar(&output, "output", "generated.png", "Output image path")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: gpt-image generate <prompt|-> [flags]

  Prompt may be "-" to read from stdin.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) < 1 {
		fs.Usage()
		return 2
	}
	prompt := rest[0]
	if prompt == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		prompt = strings.TrimSpace(string(b))
	}
	if strings.TrimSpace(prompt) == "" {
		fmt.Fprintln(os.Stderr, "prompt is empty")
		return 1
	}
	if err := common.validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	client, err := common.newClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	started := time.Now()
	result, err := client.Generate(context.Background(), prompt, common.options())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	written, err := WriteImage(result, expandHome(output))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	printWrote(written, result, common.model, time.Since(started), common.noCost)
	return 0
}

func cmdEdit(args []string) int {
	fs := flag.NewFlagSet("edit", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var common commonFlags
	var output, prompt string
	addCommonFlags(fs, &common)
	fs.StringVar(&output, "output", "edited.png", "Output image path")
	fs.StringVar(&prompt, "prompt", "", "Edit instruction (required)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: gpt-image edit <image> [image...] --prompt "..." [flags]

Flags:
`)
		fs.PrintDefaults()
	}
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	images := fs.Args()
	if len(images) < 1 {
		fs.Usage()
		return 2
	}
	if strings.TrimSpace(prompt) == "" {
		fmt.Fprintln(os.Stderr, "--prompt is required")
		return 2
	}
	if err := common.validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	paths := make([]string, len(images))
	for i, p := range images {
		paths[i] = expandHome(p)
		if st, err := os.Stat(paths[i]); err != nil || st.IsDir() {
			fmt.Fprintf(os.Stderr, "input image not found: %s\n", paths[i])
			return 1
		}
	}
	client, err := common.newClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	started := time.Now()
	result, err := client.Edit(context.Background(), paths, prompt, common.options())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	written, err := WriteImage(result, expandHome(output))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	printWrote(written, result, common.model, time.Since(started), common.noCost)
	return 0
}

func cmdHairColor(args []string) int {
	fs := flag.NewFlagSet("hair-color", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var common commonFlags
	// Defaults match the old edit_hair_color.py script.
	common.quality = "medium"
	var output, prompt, color, extra string
	addCommonFlags(fs, &common)
	fs.StringVar(&output, "output", "edited.png", "Output image path")
	fs.StringVar(&prompt, "prompt", "", "Custom edit prompt (overrides default hair-color prompt)")
	fs.StringVar(&color, "color", "strawberry blonde", "Target hair color when --prompt is not set")
	fs.StringVar(&extra, "extra-instruction", "", "Optional text appended to the edit prompt")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: gpt-image hair-color <image> [flags]

Convenience wrapper around edit for the original Arena hair-color path.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	images := fs.Args()
	if len(images) != 1 {
		fs.Usage()
		return 2
	}
	if err := common.validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	imagePath := expandHome(images[0])
	if st, err := os.Stat(imagePath); err != nil || st.IsDir() {
		fmt.Fprintf(os.Stderr, "input image not found: %s\n", imagePath)
		return 1
	}

	editPrompt := strings.TrimSpace(prompt)
	if editPrompt == "" {
		// Interactive prompt like the old script when stdin is a TTY is optional;
		// non-interactive defaults to the hair-color template.
		editPrompt = fmt.Sprintf(
			"Edit this photo realistically. Change only the subject's hair color "+
				"to natural %s with believable highlights, roots, shadows, and "+
				"strand detail. Do not change eyebrows. Preserve the face, eyes, skin "+
				"tone, expression, hands, jewelry, clothing, background, lighting, "+
				"framing, and photo-realistic texture.",
			color,
		)
	}
	if strings.TrimSpace(extra) != "" {
		editPrompt = editPrompt + " " + strings.TrimSpace(extra)
	}

	client, err := common.newClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	started := time.Now()
	result, err := client.Edit(context.Background(), []string{imagePath}, editPrompt, common.options())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	written, err := WriteImage(result, expandHome(output))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	printWrote(written, result, common.model, time.Since(started), common.noCost)
	return 0
}

type manifestItem struct {
	ID       string          `json:"id"`
	Prompt   string          `json:"prompt"`
	Output   string          `json:"output"`
	Quality  string          `json:"quality"`
	Size     string          `json:"size"`
	EditFrom json.RawMessage `json:"edit_from"`
}

func loadManifest(path string) ([]manifestItem, error) {
	b, err := os.ReadFile(expandHome(path))
	if err != nil {
		return nil, err
	}
	var raw any
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	var list []any
	switch v := raw.(type) {
	case []any:
		list = v
	case map[string]any:
		imgs, ok := v["images"]
		if !ok {
			return nil, errors.New("manifest object must contain an \"images\" list")
		}
		arr, ok := imgs.([]any)
		if !ok {
			return nil, errors.New("manifest \"images\" must be a list")
		}
		list = arr
	default:
		return nil, errors.New("manifest must be a non-empty list, or an object with an \"images\" list")
	}
	if len(list) == 0 {
		return nil, errors.New("manifest must be a non-empty list, or an object with an \"images\" list")
	}
	items := make([]manifestItem, 0, len(list))
	for i, el := range list {
		bb, err := json.Marshal(el)
		if err != nil {
			return nil, err
		}
		var item manifestItem
		if err := json.Unmarshal(bb, &item); err != nil {
			return nil, fmt.Errorf("manifest item %d: %w", i, err)
		}
		if strings.TrimSpace(item.Prompt) == "" {
			return nil, fmt.Errorf("manifest item %d is missing \"prompt\"", i)
		}
		if item.ID == "" {
			item.ID = fmt.Sprintf("image-%02d", i+1)
		}
		items = append(items, item)
	}
	return items, nil
}

func parseEditFrom(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		if one == "" {
			return nil, nil
		}
		return []string{expandHome(one)}, nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err != nil {
		return nil, fmt.Errorf("edit_from must be a string or list of strings")
	}
	out := make([]string, 0, len(many))
	for _, p := range many {
		if p != "" {
			out = append(out, expandHome(p))
		}
	}
	return out, nil
}

func cmdBatch(args []string) int {
	fs := flag.NewFlagSet("batch", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var common commonFlags
	var outputDir string
	var workers int
	var skipExisting, dryRun bool
	addCommonFlags(fs, &common)
	fs.StringVar(&outputDir, "output-dir", "outputs", "Default output directory for items without \"output\"")
	fs.IntVar(&workers, "workers", 3, "Concurrent requests")
	fs.BoolVar(&skipExisting, "skip-existing", false, "Skip outputs that already exist")
	fs.BoolVar(&dryRun, "dry-run", false, "Print the plan without calling the API")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: gpt-image batch <manifest.json> [flags]

Flags:
`)
		fs.PrintDefaults()
	}
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) != 1 {
		fs.Usage()
		return 2
	}
	if workers < 1 {
		fmt.Fprintln(os.Stderr, "--workers must be >= 1")
		return 2
	}
	if err := common.validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	items, err := loadManifest(rest[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	type job struct {
		item    manifestItem
		output  string
		options RequestOptions
		sources []string
	}
	var jobs []job
	for _, item := range items {
		out := item.Output
		if out == "" {
			out = filepath.Join(outputDir, item.ID+"."+common.outputFormat)
		}
		out = expandHome(out)
		if skipExisting {
			if st, err := os.Stat(out); err == nil && !st.IsDir() {
				fmt.Printf("skip %s (exists: %s)\n", item.ID, out)
				continue
			}
		}
		opts := common.options()
		if item.Quality != "" {
			opts.Quality = item.Quality
		}
		if item.Size != "" {
			opts.Size = item.Size
		}
		sources, err := parseEditFrom(item.EditFrom)
		if err != nil {
			fmt.Fprintf(os.Stderr, "item %s: %v\n", item.ID, err)
			return 1
		}
		jobs = append(jobs, job{item: item, output: out, options: opts, sources: sources})
	}

	if dryRun {
		for _, j := range jobs {
			mode := "generate"
			if len(j.sources) > 0 {
				mode = "edit"
			}
			fmt.Printf("%s %s -> %s [%s, %s]\n", mode, j.item.ID, j.output, j.options.Quality, j.options.Size)
			fmt.Printf("  %s\n", j.item.Prompt)
		}
		return 0
	}

	client, err := common.newClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	// Batch already prints per-item lines; keep heartbeats off to avoid noise.
	client.SetQuiet(true)

	var (
		failures  atomic.Int64
		totalCost atomic.Uint64 // store cost * 1e6 as integer for simple atomic sum
		costCount atomic.Int64
		wg        sync.WaitGroup
		sem       = make(chan struct{}, workers)
		mu        sync.Mutex // serialize stdout
	)

	for _, j := range jobs {
		j := j
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			started := time.Now()
			var result *ImageResult
			var err error
			if len(j.sources) > 0 {
				result, err = client.Edit(context.Background(), j.sources, j.item.Prompt, j.options)
			} else {
				result, err = client.Generate(context.Background(), j.item.Prompt, j.options)
			}
			if err != nil {
				failures.Add(1)
				mu.Lock()
				fmt.Fprintf(os.Stderr, "FAILED %s: %v\n", j.item.ID, err)
				mu.Unlock()
				return
			}
			written, err := WriteImage(result, j.output)
			if err != nil {
				failures.Add(1)
				mu.Lock()
				fmt.Fprintf(os.Stderr, "FAILED %s: %v\n", j.item.ID, err)
				mu.Unlock()
				return
			}
			elapsed := time.Since(started)
			line := fmt.Sprintf("done %s -> %s", j.item.ID, written)
			if !common.noCost {
				usage := SummarizeUsage(result, j.options.Model)
				if detail := usage.FormatLine(elapsed); detail != "" {
					line = line + " · " + detail
				} else {
					line = fmt.Sprintf("%s (%.0fs)", line, elapsed.Seconds())
				}
				if usage.CostUSD != nil {
					// microdollars
					totalCost.Add(uint64(*usage.CostUSD * 1_000_000))
					costCount.Add(1)
				}
			} else {
				line = fmt.Sprintf("%s (%.0fs)", line, elapsed.Seconds())
			}
			mu.Lock()
			fmt.Println(line)
			mu.Unlock()
		}()
	}
	wg.Wait()

	failN := failures.Load()
	okN := int64(len(jobs)) - failN
	summary := fmt.Sprintf("%d/%d images written to %s", okN, len(jobs), outputDir)
	if !common.noCost && costCount.Load() > 0 {
		summary += fmt.Sprintf(" · total ~$%.4f est.", float64(totalCost.Load())/1_000_000)
	}
	fmt.Println(summary)
	if failN > 0 {
		return 1
	}
	return 0
}

func printWrote(path string, result *ImageResult, model string, elapsed time.Duration, noCost bool) {
	line := fmt.Sprintf("Wrote %s", path)
	if !noCost {
		usage := SummarizeUsage(result, model)
		if detail := usage.FormatLine(elapsed); detail != "" {
			line = line + " · " + detail
		} else {
			line = fmt.Sprintf("%s (%.0fs)", line, elapsed.Seconds())
		}
	}
	fmt.Println(line)
}
