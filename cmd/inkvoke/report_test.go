package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	return buf.String()
}

func TestPrintVersionJSON(t *testing.T) {
	out := captureStdout(t, func() { printVersion(true) })
	var env resultEnvelope
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if !env.OK || env.Version != version || env.DefaultModel != defaultModel {
		t.Fatalf("envelope: %+v", env)
	}
}

func TestPrintVersionHuman(t *testing.T) {
	out := captureStdout(t, func() { printVersion(false) })
	if !strings.Contains(out, version) || !strings.Contains(out, defaultModel) {
		t.Fatalf("version line: %q", out)
	}
}

func TestEmitWroteJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.png")
	if err := os.WriteFile(path, []byte("fake-png"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := &ImageResult{
		Usage: map[string]any{
			"input_tokens":  float64(10),
			"output_tokens": float64(100),
			"input_tokens_details": map[string]any{
				"text_tokens": float64(10),
			},
			"output_tokens_details": map[string]any{
				"image_tokens": float64(100),
			},
		},
		Raw: map[string]any{
			"data": []any{
				map[string]any{"revised_prompt": "a better prompt"},
			},
		},
	}
	opts := RequestOptions{Model: defaultModel, Quality: "high", Size: "1280x648", OutputFormat: "png"}
	out := captureStdout(t, func() {
		emitWrote("generate", path, result, opts, 12*time.Second, true, false)
	})
	var env resultEnvelope
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if !env.OK || env.Command != "generate" || env.Bytes != 8 {
		t.Fatalf("envelope: %+v", env)
	}
	if env.RevisedPrompt != "a better prompt" {
		t.Fatalf("revised_prompt: %q", env.RevisedPrompt)
	}
	if env.Usage == nil || env.Usage.TextInputTokens != 10 || env.Usage.ImageOutputTokens != 100 {
		t.Fatalf("usage: %+v", env.Usage)
	}
	if env.CostUSDEstimate == nil {
		t.Fatal("expected cost")
	}
	if env.ElapsedSeconds < 11 || env.ElapsedSeconds > 13 {
		t.Fatalf("elapsed: %v", env.ElapsedSeconds)
	}
}

func TestEmitFailureJSON(t *testing.T) {
	out := captureStdout(t, func() {
		code := emitFailure("generate", usagef("prompt is empty"), true)
		if code != exitUsage {
			t.Errorf("exit %d", code)
		}
	})
	var env resultEnvelope
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if env.OK || env.Error == nil || env.Error.Code != "usage" || env.Error.Retryable {
		t.Fatalf("envelope: %+v", env)
	}
}

func TestWantsJSON(t *testing.T) {
	if !wantsJSON([]string{"--json", "hi"}) {
		t.Fatal("expected true")
	}
	if wantsJSON([]string{"--", "--json"}) {
		t.Fatal("after -- should be ignored")
	}
	if wantsJSON([]string{"--quiet"}) {
		t.Fatal("expected false")
	}
}

func TestAgentsHelpEmbedded(t *testing.T) {
	if !strings.Contains(agentsHelp, "--json") && !strings.Contains(agentsHelp, "inkvoke") {
		t.Fatal("embedded agents help looks empty")
	}
	// Keep embed in sync with root AGENTS.md when both are present.
	root := filepath.Join("..", "..", "AGENTS.md")
	b, err := os.ReadFile(root)
	if err != nil {
		t.Skip("root AGENTS.md not present")
	}
	if string(b) != agentsHelp {
		t.Fatal("cmd/inkvoke/agents.md is out of sync with AGENTS.md; copy root over embed")
	}
}

func TestCmdVersionJSON(t *testing.T) {
	code := 0
	out := captureStdout(t, func() { code = cmdVersion([]string{"--json"}) })
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var env resultEnvelope
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatal(err)
	}
	if env.DefaultModel != defaultModel {
		t.Fatalf("%+v", env)
	}
}

func TestRunHelpAgent(t *testing.T) {
	code := 0
	out := captureStdout(t, func() { code = run([]string{"--help-agent"}) })
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out, "inkvoke") {
		t.Fatalf("help-agent output: %q", out[:min(80, len(out))])
	}
}

func TestRunUsageExitCode(t *testing.T) {
	code := run([]string{})
	if code != exitUsage {
		t.Fatalf("empty args exit %d want %d", code, exitUsage)
	}
	code = run([]string{"generate"}) // missing prompt
	if code != exitUsage {
		t.Fatalf("missing prompt exit %d want %d", code, exitUsage)
	}
}

func TestRunAuthExitCode(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	// Ensure no ambient key.
	code := run([]string{"generate", "a cat", "--output", filepath.Join(t.TempDir(), "x.png"), "--quiet"})
	if code != exitAuth {
		t.Fatalf("missing key exit %d want %d", code, exitAuth)
	}
}

func TestRunJSONUsageError(t *testing.T) {
	code := 0
	out := captureStdout(t, func() {
		code = run([]string{"generate", "--json"})
	})
	if code != exitUsage {
		t.Fatalf("exit %d", code)
	}
	var env resultEnvelope
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if env.OK || env.Error == nil {
		t.Fatalf("%+v", env)
	}
}
