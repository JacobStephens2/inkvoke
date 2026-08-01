package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// Shared line reader so multi-line pastes are not lost between questions.
// Creating a new bufio.Reader per question discards buffered lines and makes
// pasted prompts "eat" the next wizard answers (output path, quality, …).
var (
	stdinMu     sync.Mutex
	stdinReader *bufio.Reader
)

func lineReader() *bufio.Reader {
	stdinMu.Lock()
	defer stdinMu.Unlock()
	if stdinReader == nil {
		stdinReader = bufio.NewReader(os.Stdin)
	}
	return stdinReader
}

// resetLineReader is for tests that replace readLineFromTerminal entirely;
// production code relies on a single reader for the process lifetime.
func resetLineReader() {
	stdinMu.Lock()
	defer stdinMu.Unlock()
	stdinReader = nil
}

// readLineFromTerminal is stubbed in tests. Prompts go to stderr; answers from stdin.
var readLineFromTerminal = func(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	line, err := lineReader().ReadString('\n')
	if err != nil {
		// Allow EOF after a non-empty line (e.g. piped single answer in tests).
		if line != "" {
			return strings.TrimRight(line, "\r\n"), nil
		}
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func canInteractive() bool {
	return stdinIsTerminal()
}

// promptLine asks one question. If required is false and the user presses Enter,
// def is returned (may be empty). If required is true, empty answers are re-asked.
func promptLine(label, def string, required bool) (string, error) {
	for {
		var prompt string
		if def != "" {
			prompt = fmt.Sprintf("%s [%s]: ", label, def)
		} else {
			prompt = fmt.Sprintf("%s: ", label)
		}
		raw, err := readLineFromTerminal(prompt)
		if err != nil {
			return "", usagef("read input: %v", err)
		}
		ans := strings.TrimSpace(raw)
		if ans == "" {
			if required && def == "" {
				fmt.Fprintln(os.Stderr, "  (required — please enter a value)")
				continue
			}
			return def, nil
		}
		return ans, nil
	}
}

// pasteIdle is how long we wait for another line after receiving one.
// Clipboard pastes arrive as a burst; a single typed line + Enter finishes
// after this idle window. Optional "---" still ends input immediately.
var pasteIdle = 300 * time.Millisecond

// stdinHasMore is stubbed in tests. Production uses poll on stdin (unix).
var stdinHasMore = func(idle time.Duration) bool {
	return stdinBuffered() > 0 || stdinReadable(idle)
}

func stdinBuffered() int {
	stdinMu.Lock()
	defer stdinMu.Unlock()
	if stdinReader == nil {
		return 0
	}
	return stdinReader.Buffered()
}

// promptMultiline collects an image/edit prompt as plain text for the API.
// Whatever the user types or pastes (including blank lines and multi-paragraph
// text) becomes the prompt string. Multi-line paste is auto-captured by
// draining stdin until idle; optional "---" or Ctrl-D also ends input.
func promptMultiline(label string) (string, error) {
	return promptMultilineStartingWith(label, "")
}

func promptMultilineStartingWith(label, firstLine string) (string, error) {
	fmt.Fprintf(os.Stderr, "%s\n", label)
	fmt.Fprintln(os.Stderr, "  (type one line + Enter, or paste any multi-line text — it is sent as-is to the API)")
	for {
		var lines []string
		if firstLine != "" {
			// Seed from Command-line paste recovery; do not re-print a prompt.
			lines = append(lines, strings.TrimRight(firstLine, "\r\n"))
			firstLine = ""
			fmt.Fprintln(os.Stderr, "  (…capturing pasted prompt)")
		} else {
			raw, err := readLineFromTerminal("> ")
			if err != nil {
				if errors.Is(err, io.EOF) {
					// empty EOF
				} else if strings.TrimSpace(raw) != "" {
					lines = append(lines, raw)
				} else {
					return "", usagef("read input: %v", err)
				}
			} else if strings.TrimSpace(raw) == "---" {
				// ignore lone terminator with no content
			} else {
				lines = append(lines, raw)
			}
		}

		// Drain remaining lines from a multi-line paste (or until --- / EOF).
		more, err := drainPromptLines()
		if err != nil {
			return "", err
		}
		lines = append(lines, more...)

		text := strings.TrimRight(strings.Join(lines, "\n"), "\r\n")
		if strings.TrimSpace(text) == "" {
			fmt.Fprintln(os.Stderr, "  (required — enter or paste a prompt)")
			continue
		}
		// One normalized string for the Images API (newlines preserved).
		return text, nil
	}
}

// drainPromptLines reads additional lines while stdin still has paste data,
// or until the user types "---" / sends EOF. Blank lines inside the paste
// are preserved as empty strings in the slice (joined with \n later).
func drainPromptLines() ([]string, error) {
	var lines []string
	for {
		if stdinBuffered() == 0 && !stdinHasMore(pasteIdle) {
			break
		}
		// No prompt prefix for drained lines — avoids garbling pasted output.
		raw, err := readLineFromTerminal("")
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			if strings.TrimSpace(raw) != "" {
				lines = append(lines, raw)
			}
			break
		}
		if strings.TrimSpace(raw) == "---" {
			break
		}
		lines = append(lines, raw)
	}
	return lines, nil
}

// promptChoice asks until the answer is one of options (case-insensitive).
// def may be empty; if set, Enter accepts def.
// If rejectLong is true and the answer is not an option but looks like a pasted
// paragraph (spaces / long), returns that text with err == errLongChoice so the
// caller can recover (e.g. treat as a generate prompt).
var errLongChoice = errors.New("long non-choice input")

func promptChoice(label string, options []string, def string) (string, error) {
	return promptChoiceOpts(label, options, def, false)
}

func promptChoiceOpts(label string, options []string, def string, rejectLong bool) (string, error) {
	optSet := make(map[string]string, len(options))
	for _, o := range options {
		optSet[strings.ToLower(o)] = o
	}
	hint := strings.Join(options, "/")
	for {
		var prompt string
		if def != "" {
			prompt = fmt.Sprintf("%s (%s) [%s]: ", label, hint, def)
		} else {
			prompt = fmt.Sprintf("%s (%s): ", label, hint)
		}
		raw, err := readLineFromTerminal(prompt)
		if err != nil {
			return "", usagef("read input: %v", err)
		}
		ans := strings.TrimSpace(raw)
		if ans == "" {
			if def != "" {
				return def, nil
			}
			fmt.Fprintln(os.Stderr, "  (required — pick one of: "+hint+")")
			continue
		}
		if canon, ok := optSet[strings.ToLower(ans)]; ok {
			return canon, nil
		}
		if rejectLong && looksLikePastedPrompt(ans) {
			return ans, errLongChoice
		}
		// Show a short preview so a pasted paragraph does not flood the terminal.
		preview := ans
		if len(preview) > 60 {
			preview = preview[:57] + "..."
		}
		fmt.Fprintf(os.Stderr, "  (unknown %q — pick one of: %s)\n", preview, hint)
	}
}

func looksLikePastedPrompt(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	// Real commands are single short tokens; pastes are sentences / multi-word.
	if strings.ContainsAny(s, " \t") && len(s) > 12 {
		return true
	}
	if len(s) > 40 {
		return true
	}
	return false
}

// runInteractiveRoot is the wizard for bare `inkvoke` with no args.
func runInteractiveRoot() int {
	fmt.Fprintln(os.Stderr, "inkvoke interactive mode (Enter accepts defaults where shown)")
	fmt.Fprintln(os.Stderr, "Tip: press Enter for generate, then type or paste your image prompt (multi-line paste is captured automatically)")
	cmd, err := promptChoiceOpts("Command", []string{"generate", "edit", "batch", "hair-color"}, "generate", true)
	if err != nil {
		if errors.Is(err, errLongChoice) {
			// User pasted the image prompt on the Command line — recover.
			fmt.Fprintln(os.Stderr, "  (looks like an image prompt — using command generate)")
			return interactiveGenerateStartingWith(cmd)
		}
		return emitFailure("", err, false)
	}
	switch cmd {
	case "generate":
		return interactiveGenerate()
	case "edit":
		return interactiveEdit()
	case "batch":
		return interactiveBatch()
	case "hair-color":
		return interactiveHairColor()
	default:
		return emitFailure("", usagef("unknown command %q", cmd), false)
	}
}

func interactiveGenerate() int {
	return interactiveGenerateStartingWith("")
}

func interactiveGenerateStartingWith(firstLine string) int {
	fmt.Fprintln(os.Stderr, "Generate an image from a prompt.")
	prompt, err := promptMultilineStartingWith("Image prompt", firstLine)
	if err != nil {
		return emitFailure("generate", err, false)
	}
	output, err := promptLine("Output path", "generated.png", false)
	if err != nil {
		return emitFailure("generate", err, false)
	}
	quality, err := promptChoice("Quality", []string{"auto", "low", "medium", "high"}, "auto")
	if err != nil {
		return emitFailure("generate", err, false)
	}
	size, err := promptLine("Size (auto, 1024x1024, 1536x1024, 1024x1536, or WIDTHxHEIGHT)", "auto", false)
	if err != nil {
		return emitFailure("generate", err, false)
	}
	format, err := promptChoice("Output format", []string{"png", "jpeg", "webp"}, "png")
	if err != nil {
		return emitFailure("generate", err, false)
	}
	args := []string{
		prompt,
		"--output", output,
		"--quality", quality,
		"--size", size,
		"--output-format", format,
	}
	return cmdGenerate(args)
}

// fillGenerateInteractively prompts for a missing generate prompt (and optional
// overrides) when the user ran `inkvoke generate` without a prompt on a TTY.
func fillGenerateInteractively(output *string, common *commonFlags) (prompt string, err error) {
	fmt.Fprintln(os.Stderr, "Missing required args — answer each prompt (Enter accepts defaults).")
	prompt, err = promptMultiline("Image prompt")
	if err != nil {
		return "", err
	}
	out, err := promptLine("Output path", *output, false)
	if err != nil {
		return "", err
	}
	*output = out
	q, err := promptChoice("Quality", []string{"auto", "low", "medium", "high"}, common.quality)
	if err != nil {
		return "", err
	}
	common.quality = q
	sz, err := promptLine("Size (auto, 1024x1024, 1536x1024, 1024x1536, or WIDTHxHEIGHT)", common.size, false)
	if err != nil {
		return "", err
	}
	common.size = sz
	outFmt, err := promptChoice("Output format", []string{"png", "jpeg", "webp"}, common.outputFormat)
	if err != nil {
		return "", err
	}
	common.outputFormat = outFmt
	return prompt, nil
}

func interactiveEdit() int {
	fmt.Fprintln(os.Stderr, "Edit one or more existing images.")
	var images []string
	for {
		label := "Input image path"
		if len(images) > 0 {
			label = "Another input image path (Enter when done)"
		}
		required := len(images) == 0
		p, err := promptLine(label, "", required)
		if err != nil {
			return emitFailure("edit", err, false)
		}
		if p == "" {
			break
		}
		images = append(images, p)
	}
	prompt, err := promptMultiline("Edit instruction")
	if err != nil {
		return emitFailure("edit", err, false)
	}
	output, err := promptLine("Output path", "edited.png", false)
	if err != nil {
		return emitFailure("edit", err, false)
	}
	quality, err := promptChoice("Quality", []string{"auto", "low", "medium", "high"}, "auto")
	if err != nil {
		return emitFailure("edit", err, false)
	}
	size, err := promptLine("Size", "auto", false)
	if err != nil {
		return emitFailure("edit", err, false)
	}
	format, err := promptChoice("Output format", []string{"png", "jpeg", "webp"}, "png")
	if err != nil {
		return emitFailure("edit", err, false)
	}
	args := append([]string{}, images...)
	args = append(args,
		"--prompt", prompt,
		"--output", output,
		"--quality", quality,
		"--size", size,
		"--output-format", format,
	)
	return cmdEdit(args)
}

func fillEditInteractively(images *[]string, prompt *string, output *string, common *commonFlags) error {
	fmt.Fprintln(os.Stderr, "Missing required args — answer each prompt (Enter accepts defaults).")
	if len(*images) < 1 {
		for {
			label := "Input image path"
			if len(*images) > 0 {
				label = "Another input image path (Enter when done)"
			}
			required := len(*images) == 0
			p, err := promptLine(label, "", required)
			if err != nil {
				return err
			}
			if p == "" {
				break
			}
			*images = append(*images, p)
		}
	}
	if strings.TrimSpace(*prompt) == "" {
		p, err := promptMultiline("Edit instruction")
		if err != nil {
			return err
		}
		*prompt = p
	}
	out, err := promptLine("Output path", *output, false)
	if err != nil {
		return err
	}
	*output = out
	q, err := promptChoice("Quality", []string{"auto", "low", "medium", "high"}, common.quality)
	if err != nil {
		return err
	}
	common.quality = q
	return nil
}

func interactiveBatch() int {
	fmt.Fprintln(os.Stderr, "Batch from a JSON manifest.")
	manifest, err := promptLine("Manifest path (JSON)", "", true)
	if err != nil {
		return emitFailure("batch", err, false)
	}
	outputDir, err := promptLine("Output directory", "outputs", false)
	if err != nil {
		return emitFailure("batch", err, false)
	}
	return cmdBatch([]string{manifest, "--output-dir", outputDir})
}

func fillBatchInteractively() (manifest string, err error) {
	fmt.Fprintln(os.Stderr, "Missing required args — answer each prompt.")
	return promptLine("Manifest path (JSON)", "", true)
}

func interactiveHairColor() int {
	fmt.Fprintln(os.Stderr, "Hair-color edit (legacy Arena path).")
	image, err := promptLine("Input image path", "", true)
	if err != nil {
		return emitFailure("hair-color", err, false)
	}
	color, err := promptLine("Hair color", "strawberry blonde", false)
	if err != nil {
		return emitFailure("hair-color", err, false)
	}
	output, err := promptLine("Output path", "edited.png", false)
	if err != nil {
		return emitFailure("hair-color", err, false)
	}
	return cmdHairColor([]string{image, "--color", color, "--output", output})
}

func fillHairColorInteractively() (image string, err error) {
	fmt.Fprintln(os.Stderr, "Missing required args — answer each prompt.")
	return promptLine("Input image path", "", true)
}
