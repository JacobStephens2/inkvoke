package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// defaultModel is what generate/edit/batch use when --model is omitted.
// version reports this so agents know what the next call will spend money on.
const defaultModel = "gpt-image-2"

// resultEnvelope is the single JSON contract for every command under --json.
// See docs/specs/agent-interface.md. One flag, one schema; human stdout is free to change.
type resultEnvelope struct {
	OK              bool            `json:"ok"`
	Command         string          `json:"command,omitempty"`
	ID              string          `json:"id,omitempty"`
	Output          string          `json:"output,omitempty"`
	Bytes           int64           `json:"bytes,omitempty"`
	Model           string          `json:"model,omitempty"`
	Size            string          `json:"size,omitempty"`
	Quality         string          `json:"quality,omitempty"`
	OutputFormat    string          `json:"output_format,omitempty"`
	ElapsedSeconds  float64         `json:"elapsed_seconds,omitempty"`
	Usage           *usageEnvelope  `json:"usage,omitempty"`
	CostUSDEstimate *float64        `json:"cost_usd_estimate,omitempty"`
	RevisedPrompt   string          `json:"revised_prompt,omitempty"`
	Skipped         bool            `json:"skipped,omitempty"`
	Results         []resultEnvelope `json:"results,omitempty"`
	Error           *errorEnvelope  `json:"error,omitempty"`
	Version         string          `json:"version,omitempty"`
	DefaultModel    string          `json:"default_model,omitempty"`
	DryRun          bool            `json:"dry_run,omitempty"`
	Mode            string          `json:"mode,omitempty"` // batch dry-run: generate|edit
	Prompt          string          `json:"prompt,omitempty"`
}

type usageEnvelope struct {
	TextInputTokens   int `json:"text_input_tokens,omitempty"`
	ImageInputTokens  int `json:"image_input_tokens,omitempty"`
	ImageOutputTokens int `json:"image_output_tokens,omitempty"`
	TextOutputTokens  int `json:"text_output_tokens,omitempty"`
	InputTokens       int `json:"input_tokens,omitempty"`
	OutputTokens      int `json:"output_tokens,omitempty"`
}

type errorEnvelope struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

// errSprintf is the shared format helper for exitcode.go constructors.
func errSprintf(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}

func writeJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v) // trailing newline included
}

func absPath(p string) string {
	if p == "" {
		return p
	}
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

func fileSize(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}

func usageFromSummary(u UsageSummary) *usageEnvelope {
	if u.InputTokens == 0 && u.OutputTokens == 0 &&
		u.TextInput == 0 && u.ImageInput == 0 && u.ImageOutput == 0 && u.TextOutput == 0 {
		return nil
	}
	return &usageEnvelope{
		TextInputTokens:   u.TextInput,
		ImageInputTokens:  u.ImageInput,
		ImageOutputTokens: u.ImageOutput,
		TextOutputTokens:  u.TextOutput,
		InputTokens:       u.InputTokens,
		OutputTokens:      u.OutputTokens,
	}
}

func revisedPromptOf(result *ImageResult) string {
	if result == nil || result.Raw == nil {
		return ""
	}
	data, _ := result.Raw["data"].([]any)
	if len(data) == 0 {
		return ""
	}
	first, _ := data[0].(map[string]any)
	s, _ := first["revised_prompt"].(string)
	return s
}

// emitWrote prints the human success line or a --json success envelope.
func emitWrote(command, path string, result *ImageResult, opts RequestOptions, elapsed time.Duration, jsonMode, noCost bool) {
	path = absPath(path)
	if jsonMode {
		env := resultEnvelope{
			OK:             true,
			Command:        command,
			Output:         path,
			Bytes:          fileSize(path),
			Model:          opts.Model,
			Size:           opts.Size,
			Quality:        opts.Quality,
			OutputFormat:   opts.OutputFormat,
			ElapsedSeconds: elapsed.Seconds(),
			RevisedPrompt:  revisedPromptOf(result),
		}
		if !noCost {
			u := SummarizeUsage(result, opts.Model)
			env.Usage = usageFromSummary(u)
			env.CostUSDEstimate = u.CostUSD
		}
		writeJSON(env)
		return
	}
	printWrote(path, result, opts.Model, elapsed, noCost)
}

// emitFailure prints a --json error envelope (when jsonMode) and/or a stderr line,
// and returns the classified exit code.
func emitFailure(command string, err error, jsonMode bool) int {
	c := classedOf(err)
	if c.Exit == exitOK {
		return exitOK
	}
	if jsonMode {
		writeJSON(resultEnvelope{
			OK:      false,
			Command: command,
			Error: &errorEnvelope{
				Code:      c.Code,
				Message:   c.Message,
				Retryable: c.Retryable,
			},
		})
	} else {
		fmt.Fprintln(os.Stderr, c.Message)
	}
	return c.Exit
}

// emitBatchJSON writes the batch envelope (one document for the whole run).
func emitBatchJSON(results []resultEnvelope, totalCost *float64, ok bool) {
	env := resultEnvelope{
		OK:      ok,
		Command: "batch",
		Results: results,
	}
	if totalCost != nil {
		env.CostUSDEstimate = totalCost
	}
	var maxElapsed float64
	for _, r := range results {
		if r.ElapsedSeconds > maxElapsed {
			maxElapsed = r.ElapsedSeconds
		}
	}
	if maxElapsed > 0 {
		env.ElapsedSeconds = maxElapsed
	}
	writeJSON(env)
}

func itemSuccess(id, path string, result *ImageResult, opts RequestOptions, elapsed time.Duration, noCost bool) resultEnvelope {
	path = absPath(path)
	env := resultEnvelope{
		OK:             true,
		ID:             id,
		Output:         path,
		Bytes:          fileSize(path),
		Model:          opts.Model,
		Size:           opts.Size,
		Quality:        opts.Quality,
		OutputFormat:   opts.OutputFormat,
		ElapsedSeconds: elapsed.Seconds(),
		RevisedPrompt:  revisedPromptOf(result),
	}
	if !noCost {
		u := SummarizeUsage(result, opts.Model)
		env.Usage = usageFromSummary(u)
		env.CostUSDEstimate = u.CostUSD
	}
	return env
}

func itemSkipped(id, path string) resultEnvelope {
	return resultEnvelope{
		OK:      true,
		ID:      id,
		Output:  absPath(path),
		Skipped: true,
	}
}

func itemFailure(id string, err error) resultEnvelope {
	c := classedOf(err)
	return resultEnvelope{
		OK: false,
		ID: id,
		Error: &errorEnvelope{
			Code:      c.Code,
			Message:   c.Message,
			Retryable: c.Retryable,
		},
	}
}

func printVersion(jsonMode bool) {
	if jsonMode {
		writeJSON(resultEnvelope{
			OK:           true,
			Command:      "version",
			Version:      version,
			DefaultModel: defaultModel,
		})
		return
	}
	fmt.Printf("gpt-image %s (default model: %s)\n", version, defaultModel)
}

// wantsJSON reports whether args contain an orthogonal --json flag (before --).
func wantsJSON(args []string) bool {
	for _, a := range args {
		if a == "--" {
			break
		}
		if a == "--json" {
			return true
		}
	}
	return false
}
