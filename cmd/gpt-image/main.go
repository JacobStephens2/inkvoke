// Command gpt-image is a CLI for OpenAI's image models (gpt-image-2 by default):
// generate from a prompt, edit existing photos, or run a JSON batch.
package main

import (
	"context"
	_ "embed"
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

	"golang.org/x/term"
)

//go:embed agents.md
var agentsHelp string

const version = "0.3.2"

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
		// Humans: progressive wizard. Agents/CI (non-TTY): usage help + exit 1.
		if canInteractive() {
			return runInteractiveRoot()
		}
		printRootHelp(os.Stderr)
		return exitUsage
	}
	switch args[0] {
	case "-h", "--help", "help":
		printRootHelp(os.Stdout)
		return exitOK
	case "--help-agent", "help-agent":
		fmt.Print(agentsHelp)
		if !strings.HasSuffix(agentsHelp, "\n") {
			fmt.Println()
		}
		return exitOK
	case "-v", "--version":
		printVersion(false)
		return exitOK
	case "version":
		return cmdVersion(args[1:])
	case "--uninstall", "uninstall":
		return cmdUninstall(args[1:])
	case "generate":
		return cmdGenerate(args[1:])
	case "edit":
		return cmdEdit(args[1:])
	case "batch":
		return cmdBatch(args[1:])
	case "hair-color":
		return cmdHairColor(args[1:])
	default:
		msg := fmt.Sprintf("unknown command %q", args[0])
		if wantsJSON(args) {
			return emitFailure("", &usageError{msg: msg}, true)
		}
		fmt.Fprintf(os.Stderr, "%s\n\n", msg)
		printRootHelp(os.Stderr)
		return exitUsage
	}
}

func cmdVersion(args []string) int {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var jsonMode bool
	fs.BoolVar(&jsonMode, "json", false, "Emit a single JSON object on stdout")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: gpt-image version [--json]

Flags:
`)
		fs.PrintDefaults()
	}
	if err := parseFlags(fs, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return emitFailure("version", &usageError{msg: err.Error()}, wantsJSON(args))
	}
	printVersion(jsonMode)
	return exitOK
}

func printRootHelp(w io.Writer) {
	fmt.Fprint(w, `gpt-image - generate and edit images with OpenAI image models

Usage:
  gpt-image <command> [flags]
  gpt-image                 Interactive mode (TTY): ask for missing info step by step

Commands:
  generate    Generate an image from a prompt
  edit        Edit one or more input images with a prompt
  batch       Generate or edit many images from a JSON manifest
  hair-color  Convenience edit for hair color (legacy Arena path)
  version     Print version and default model
  uninstall   Remove this gpt-image binary from disk

  With no command (or a command missing required args) on a terminal, gpt-image
  prompts one question at a time. Non-interactive runs still require full flags.

Global:
  -h, --help        Show help
  -v, --version     Show version
  --help-agent      Agent-oriented usage (flags, latency, --json)
  --uninstall       Same as uninstall (remove this binary)

Exit codes:
  0  success
  1  usage error (bad flag, missing prompt, unreadable invocation)
  2  missing or rejected API key
  3  API error, non-retryable (content policy, unsupported size)
  4  API error, retryable (rate limit, 5xx, timeout)
  5  local I/O error writing or reading a file

Environment:
  OPENAI_API_KEY   API key (or pass --api-key-file, or interactive prompt)

Examples:
  gpt-image generate "a lighthouse in a storm, gouache" --output lighthouse.png --quality high --size 1536x1024
  gpt-image generate "…" --output out.jpg --size 1280x648 --output-format jpeg --json
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
	jsonMode     bool
	timeoutSec   int
}

func addCommonFlags(fs *flag.FlagSet, c *commonFlags) {
	// Pre-set fields on c become flag defaults (hair-color uses quality medium).
	modelDef, qualityDef, sizeDef, formatDef := c.model, c.quality, c.size, c.outputFormat
	if modelDef == "" {
		modelDef = defaultModel
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
	fs.StringVar(&c.apiKeyFile, "api-key-file", "", "File containing the OpenAI API key (else OPENAI_API_KEY, else interactive prompt)")
	fs.BoolVar(&c.noCost, "no-cost", false, "Do not print estimated USD cost / token usage")
	fs.BoolVar(&c.quiet, "quiet", false, "Suppress progress heartbeats on stderr")
	fs.BoolVar(&c.jsonMode, "json", false, "Emit exactly one JSON result object on stdout (heartbeats stay on stderr)")
	fs.IntVar(&c.timeoutSec, "timeout", timeoutDef, "HTTP timeout in seconds per API attempt (high quality often needs 180+)")
}

func (c *commonFlags) validate() error {
	if !qualities[c.quality] {
		return usagef("invalid --quality %q (want low|medium|high|auto)", c.quality)
	}
	if !formats[c.outputFormat] {
		return usagef("invalid --output-format %q (want png|jpeg|webp)", c.outputFormat)
	}
	// Allow documented presets or free-form WIDTHxHEIGHT (gpt-image-2).
	if !sizes[c.size] {
		var w, h int
		if _, err := fmt.Sscanf(c.size, "%dx%d", &w, &h); err != nil || w < 1 || h < 1 {
			return usagef("invalid --size %q", c.size)
		}
	}
	if c.timeoutSec < 30 {
		return usagef("invalid --timeout %d (want >= 30 seconds)", c.timeoutSec)
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

// stdinIsTerminal and readSecretFromTerminal are vars so tests can stub them.
// Interactive keys are process-memory only for this invocation — never written to disk
// and never exported into the parent shell.
var (
	stdinIsTerminal = func() bool {
		return term.IsTerminal(int(os.Stdin.Fd()))
	}
	readSecretFromTerminal = func() (string, error) {
		// Prompt and echo-free read on the controlling terminal (stdin when TTY).
		fmt.Fprint(os.Stderr, "OpenAI API key (not saved): ")
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr) // finish the input line after hidden typing
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	}
)

func resolveAPIKey(path string) (string, error) {
	if path != "" {
		b, err := os.ReadFile(expandHome(path))
		if err != nil {
			return "", authf("read --api-key-file: %v", err)
		}
		key := strings.TrimSpace(string(b))
		if key == "" {
			return "", authf("API key file is empty")
		}
		return key, nil
	}
	key := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if key != "" {
		return key, nil
	}
	// Interactive humans: prompt once for this process. Agents/CI (non-TTY) fail closed.
	if stdinIsTerminal() {
		raw, err := readSecretFromTerminal()
		if err != nil {
			return "", authf("read API key: %v", err)
		}
		key = strings.TrimSpace(raw)
		if key == "" {
			return "", authf("missing API key: empty key entered (set OPENAI_API_KEY or pass --api-key-file)")
		}
		return key, nil
	}
	return "", authf("missing API key: set OPENAI_API_KEY, pass --api-key-file /path/to/key.txt, or run in a terminal to be prompted")
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
	const command = "generate"
	jsonGuess := wantsJSON(args)
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
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
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return emitFailure(command, &usageError{msg: err.Error()}, jsonGuess || common.jsonMode)
	}
	jsonMode := common.jsonMode
	rest := fs.Args()
	var prompt string
	if len(rest) >= 1 {
		prompt = rest[0]
	}
	if prompt == "-" {
		// "-" means read prompt from stdin; not compatible with the interactive wizard.
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return emitFailure(command, iof("read stdin: %v", err), jsonMode)
		}
		prompt = strings.TrimSpace(string(b))
	}
	if strings.TrimSpace(prompt) == "" {
		if jsonMode || !canInteractive() {
			if jsonMode {
				return emitFailure(command, usagef("missing prompt"), true)
			}
			fs.Usage()
			return exitUsage
		}
		filled, err := fillGenerateInteractively(&output, &common)
		if err != nil {
			return emitFailure(command, err, false)
		}
		prompt = filled
	}
	if strings.TrimSpace(prompt) == "" {
		return emitFailure(command, usagef("prompt is empty"), jsonMode)
	}
	if err := common.validate(); err != nil {
		return emitFailure(command, &usageError{msg: err.Error()}, jsonMode)
	}
	client, err := common.newClient()
	if err != nil {
		return emitFailure(command, err, jsonMode)
	}
	started := time.Now()
	opts := common.options()
	result, err := client.Generate(context.Background(), prompt, opts)
	if err != nil {
		return emitFailure(command, err, jsonMode)
	}
	written, err := WriteImage(result, expandHome(output))
	if err != nil {
		return emitFailure(command, iof("%v", err), jsonMode)
	}
	emitWrote(command, written, result, opts, time.Since(started), jsonMode, common.noCost)
	return exitOK
}

func cmdEdit(args []string) int {
	const command = "edit"
	jsonGuess := wantsJSON(args)
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
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
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return emitFailure(command, &usageError{msg: err.Error()}, jsonGuess || common.jsonMode)
	}
	jsonMode := common.jsonMode
	images := fs.Args()
	if len(images) < 1 || strings.TrimSpace(prompt) == "" {
		if jsonMode || !canInteractive() {
			if len(images) < 1 {
				if jsonMode {
					return emitFailure(command, usagef("missing input image"), true)
				}
				fs.Usage()
				return exitUsage
			}
			return emitFailure(command, usagef("--prompt is required"), jsonMode)
		}
		if err := fillEditInteractively(&images, &prompt, &output, &common); err != nil {
			return emitFailure(command, err, false)
		}
	}
	if len(images) < 1 {
		return emitFailure(command, usagef("missing input image"), jsonMode)
	}
	if strings.TrimSpace(prompt) == "" {
		return emitFailure(command, usagef("--prompt is required"), jsonMode)
	}
	if err := common.validate(); err != nil {
		return emitFailure(command, &usageError{msg: err.Error()}, jsonMode)
	}
	paths := make([]string, len(images))
	for i, p := range images {
		paths[i] = expandHome(p)
		if st, err := os.Stat(paths[i]); err != nil || st.IsDir() {
			return emitFailure(command, iof("input image not found: %s", paths[i]), jsonMode)
		}
	}
	client, err := common.newClient()
	if err != nil {
		return emitFailure(command, err, jsonMode)
	}
	started := time.Now()
	opts := common.options()
	result, err := client.Edit(context.Background(), paths, prompt, opts)
	if err != nil {
		return emitFailure(command, err, jsonMode)
	}
	written, err := WriteImage(result, expandHome(output))
	if err != nil {
		return emitFailure(command, iof("%v", err), jsonMode)
	}
	emitWrote(command, written, result, opts, time.Since(started), jsonMode, common.noCost)
	return exitOK
}

func cmdHairColor(args []string) int {
	const command = "hair-color"
	jsonGuess := wantsJSON(args)
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
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
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return emitFailure(command, &usageError{msg: err.Error()}, jsonGuess || common.jsonMode)
	}
	jsonMode := common.jsonMode
	images := fs.Args()
	if len(images) != 1 {
		if jsonMode || !canInteractive() {
			if jsonMode {
				return emitFailure(command, usagef("hair-color expects exactly one input image"), true)
			}
			fs.Usage()
			return exitUsage
		}
		img, err := fillHairColorInteractively()
		if err != nil {
			return emitFailure(command, err, false)
		}
		images = []string{img}
		if strings.TrimSpace(color) == "strawberry blonde" {
			// Offer override when wizard-filling the only missing image path.
			c, err := promptLine("Hair color", color, false)
			if err != nil {
				return emitFailure(command, err, false)
			}
			color = c
		}
		if output == "edited.png" {
			o, err := promptLine("Output path", output, false)
			if err != nil {
				return emitFailure(command, err, false)
			}
			output = o
		}
	}
	if len(images) != 1 {
		return emitFailure(command, usagef("hair-color expects exactly one input image"), jsonMode)
	}
	if err := common.validate(); err != nil {
		return emitFailure(command, &usageError{msg: err.Error()}, jsonMode)
	}
	imagePath := expandHome(images[0])
	if st, err := os.Stat(imagePath); err != nil || st.IsDir() {
		return emitFailure(command, iof("input image not found: %s", imagePath), jsonMode)
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
		return emitFailure(command, err, jsonMode)
	}
	started := time.Now()
	opts := common.options()
	result, err := client.Edit(context.Background(), []string{imagePath}, editPrompt, opts)
	if err != nil {
		return emitFailure(command, err, jsonMode)
	}
	written, err := WriteImage(result, expandHome(output))
	if err != nil {
		return emitFailure(command, iof("%v", err), jsonMode)
	}
	emitWrote(command, written, result, opts, time.Since(started), jsonMode, common.noCost)
	return exitOK
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
		return nil, fmt.Errorf("read manifest: %w", err)
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
	const command = "batch"
	jsonGuess := wantsJSON(args)
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
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
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return emitFailure(command, &usageError{msg: err.Error()}, jsonGuess || common.jsonMode)
	}
	jsonMode := common.jsonMode
	rest := fs.Args()
	if len(rest) != 1 {
		if jsonMode || !canInteractive() {
			if jsonMode {
				return emitFailure(command, usagef("batch expects a manifest.json path"), true)
			}
			fs.Usage()
			return exitUsage
		}
		m, err := fillBatchInteractively()
		if err != nil {
			return emitFailure(command, err, false)
		}
		rest = []string{m}
		od, err := promptLine("Output directory", outputDir, false)
		if err != nil {
			return emitFailure(command, err, false)
		}
		outputDir = od
	}
	if len(rest) != 1 {
		return emitFailure(command, usagef("batch expects a manifest.json path"), jsonMode)
	}
	if workers < 1 {
		return emitFailure(command, usagef("--workers must be >= 1"), jsonMode)
	}
	if err := common.validate(); err != nil {
		return emitFailure(command, &usageError{msg: err.Error()}, jsonMode)
	}
	items, err := loadManifest(rest[0])
	if err != nil {
		// Manifest parse/read failures are usage or io depending on cause.
		if errors.Is(err, os.ErrNotExist) {
			return emitFailure(command, iof("%v", err), jsonMode)
		}
		return emitFailure(command, usagef("%v", err), jsonMode)
	}

	type job struct {
		item    manifestItem
		output  string
		options RequestOptions
		sources []string
	}
	var (
		jobs    []job
		results []resultEnvelope // preserved order for --json (includes skips)
	)
	for _, item := range items {
		out := item.Output
		if out == "" {
			out = filepath.Join(outputDir, item.ID+"."+common.outputFormat)
		}
		out = expandHome(out)
		if skipExisting {
			if st, err := os.Stat(out); err == nil && !st.IsDir() {
				if jsonMode {
					results = append(results, itemSkipped(item.ID, out))
				} else {
					fmt.Printf("skip %s (exists: %s)\n", item.ID, out)
				}
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
			return emitFailure(command, usagef("item %s: %v", item.ID, err), jsonMode)
		}
		jobs = append(jobs, job{item: item, output: out, options: opts, sources: sources})
	}

	if dryRun {
		if jsonMode {
			plan := make([]resultEnvelope, 0, len(jobs))
			for _, j := range jobs {
				mode := "generate"
				if len(j.sources) > 0 {
					mode = "edit"
				}
				plan = append(plan, resultEnvelope{
					OK:      true,
					ID:      j.item.ID,
					Output:  absPath(j.output),
					Model:   j.options.Model,
					Size:    j.options.Size,
					Quality: j.options.Quality,
					Mode:    mode,
					Prompt:  j.item.Prompt,
				})
			}
			writeJSON(resultEnvelope{OK: true, Command: command, DryRun: true, Results: plan})
			return exitOK
		}
		for _, j := range jobs {
			mode := "generate"
			if len(j.sources) > 0 {
				mode = "edit"
			}
			fmt.Printf("%s %s -> %s [%s, %s]\n", mode, j.item.ID, j.output, j.options.Quality, j.options.Size)
			fmt.Printf("  %s\n", j.item.Prompt)
		}
		return exitOK
	}

	client, err := common.newClient()
	if err != nil {
		return emitFailure(command, err, jsonMode)
	}
	// Batch already prints per-item lines; keep heartbeats off to avoid noise.
	client.SetQuiet(true)

	var (
		failures  atomic.Int64
		totalCost atomic.Uint64 // store cost * 1e6 as integer for simple atomic sum
		costCount atomic.Int64
		wg        sync.WaitGroup
		sem       = make(chan struct{}, workers)
		mu        sync.Mutex // serialize stdout and results append
		// jobResults keyed by id for stable merge after wait (skips already in results).
		jobResults = make(map[string]resultEnvelope, len(jobs))
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
				if jsonMode {
					jobResults[j.item.ID] = itemFailure(j.item.ID, err)
				} else {
					fmt.Fprintf(os.Stderr, "FAILED %s: %v\n", j.item.ID, err)
				}
				mu.Unlock()
				return
			}
			written, err := WriteImage(result, j.output)
			if err != nil {
				failures.Add(1)
				mu.Lock()
				if jsonMode {
					jobResults[j.item.ID] = itemFailure(j.item.ID, iof("%v", err))
				} else {
					fmt.Fprintf(os.Stderr, "FAILED %s: %v\n", j.item.ID, err)
				}
				mu.Unlock()
				return
			}
			elapsed := time.Since(started)
			if jsonMode {
				env := itemSuccess(j.item.ID, written, result, j.options, elapsed, common.noCost)
				if env.CostUSDEstimate != nil {
					totalCost.Add(uint64(*env.CostUSDEstimate * 1_000_000))
					costCount.Add(1)
				}
				mu.Lock()
				jobResults[j.item.ID] = env
				mu.Unlock()
				return
			}
			line := fmt.Sprintf("done %s -> %s", j.item.ID, written)
			if !common.noCost {
				usage := SummarizeUsage(result, j.options.Model)
				if detail := usage.FormatLine(elapsed); detail != "" {
					line = line + " · " + detail
				} else {
					line = fmt.Sprintf("%s (%.0fs)", line, elapsed.Seconds())
				}
				if usage.CostUSD != nil {
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
	if jsonMode {
		// Rebuild full results in manifest order: skips already appended, then jobs.
		// Skips were recorded first; append job outcomes in jobs order after re-walking items.
		full := make([]resultEnvelope, 0, len(items))
		for _, item := range items {
			if r, ok := jobResults[item.ID]; ok {
				full = append(full, r)
				continue
			}
			// Must be a skip recorded earlier.
			for _, r := range results {
				if r.ID == item.ID {
					full = append(full, r)
					break
				}
			}
		}
		var total *float64
		if !common.noCost && costCount.Load() > 0 {
			t := float64(totalCost.Load()) / 1_000_000
			total = &t
		}
		emitBatchJSON(full, total, failN == 0)
		if failN > 0 {
			return exitAPIPermanent // partial batch failure: non-zero; agents inspect results[].error
		}
		return exitOK
	}

	okN := int64(len(jobs)) - failN
	summary := fmt.Sprintf("%d/%d images written to %s", okN, len(jobs), outputDir)
	if !common.noCost && costCount.Load() > 0 {
		summary += fmt.Sprintf(" · total ~$%.4f est.", float64(totalCost.Load())/1_000_000)
	}
	fmt.Println(summary)
	if failN > 0 {
		return exitAPIPermanent
	}
	return exitOK
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
