package main

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func withLineAnswers(t *testing.T, answers ...string) {
	t.Helper()
	origLine := readLineFromTerminal
	origTerm := stdinIsTerminal
	i := 0
	readLineFromTerminal = func(prompt string) (string, error) {
		if i >= len(answers) {
			t.Fatalf("unexpected prompt %q (no more answers)", prompt)
		}
		ans := answers[i]
		i++
		return ans, nil
	}
	stdinIsTerminal = func() bool { return true }
	t.Cleanup(func() {
		readLineFromTerminal = origLine
		stdinIsTerminal = origTerm
	})
}

func TestPromptLineRequiredRejectsEmpty(t *testing.T) {
	withLineAnswers(t, "", "  hello  ")
	got, err := promptLine("Prompt", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello" {
		t.Fatalf("got %q", got)
	}
}

func TestPromptLineDefault(t *testing.T) {
	withLineAnswers(t, "")
	got, err := promptLine("Output", "generated.png", false)
	if err != nil {
		t.Fatal(err)
	}
	if got != "generated.png" {
		t.Fatalf("got %q", got)
	}
}

func TestPromptChoice(t *testing.T) {
	withLineAnswers(t, "nope", "HIGH")
	got, err := promptChoice("Quality", []string{"auto", "low", "medium", "high"}, "auto")
	if err != nil {
		t.Fatal(err)
	}
	if got != "high" {
		t.Fatalf("got %q", got)
	}
}

func TestFillGenerateInteractively(t *testing.T) {
	// Multi-line paste: first line + drained lines; stdinHasMore controls drain.
	withLineAnswers(t, "a lighthouse", "in a storm", "out.png", "high", "1536x1024", "jpeg")
	withPasteLines(t, 1) // after first line, one more available then idle
	output := "generated.png"
	common := commonFlags{quality: "auto", size: "auto", outputFormat: "png"}
	prompt, err := fillGenerateInteractively(&output, &common)
	if err != nil {
		t.Fatal(err)
	}
	if prompt != "a lighthouse\nin a storm" {
		t.Fatalf("prompt %q", prompt)
	}
	if output != "out.png" || common.quality != "high" || common.size != "1536x1024" || common.outputFormat != "jpeg" {
		t.Fatalf("output=%q common=%+v", output, common)
	}
}

func TestPromptMultilineKeepsBlankLines(t *testing.T) {
	// first line, then drain two more (blank + para two)
	withLineAnswers(t, "para one", "", "para two")
	withPasteLines(t, 2)
	got, err := promptMultiline("Image prompt")
	if err != nil {
		t.Fatal(err)
	}
	want := "para one\n\npara two"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPromptMultilineSingleLine(t *testing.T) {
	withLineAnswers(t, "just one line")
	withPasteLines(t, 0) // idle immediately after first line
	got, err := promptMultiline("Image prompt")
	if err != nil {
		t.Fatal(err)
	}
	if got != "just one line" {
		t.Fatalf("got %q", got)
	}
}

func TestPromptMultilineRejectsEmpty(t *testing.T) {
	withLineAnswers(t, "", "ok")
	withPasteLines(t, 0)
	// empty first attempt re-asks; second is "ok"
	// need paste idle for both - withPasteLines only for drains after non-empty
	orig := stdinHasMore
	stdinHasMore = func(time.Duration) bool { return false }
	t.Cleanup(func() { stdinHasMore = orig })
	got, err := promptMultiline("Image prompt")
	if err != nil {
		t.Fatal(err)
	}
	if got != "ok" {
		t.Fatalf("got %q", got)
	}
}

// withPasteLines stubs stdinHasMore to return true n times (for drain), then false.
func withPasteLines(t *testing.T, n int) {
	t.Helper()
	orig := stdinHasMore
	left := n
	stdinHasMore = func(time.Duration) bool {
		if left > 0 {
			left--
			return true
		}
		return false
	}
	t.Cleanup(func() { stdinHasMore = orig })
}

func TestRunEmptyArgsNonInteractiveStillUsage(t *testing.T) {
	orig := stdinIsTerminal
	stdinIsTerminal = func() bool { return false }
	t.Cleanup(func() { stdinIsTerminal = orig })
	code := run([]string{})
	if code != exitUsage {
		t.Fatalf("exit %d want %d", code, exitUsage)
	}
}

func TestRunGenerateMissingPromptNonInteractive(t *testing.T) {
	orig := stdinIsTerminal
	stdinIsTerminal = func() bool { return false }
	t.Cleanup(func() { stdinIsTerminal = orig })
	code := run([]string{"generate"})
	if code != exitUsage {
		t.Fatalf("exit %d want %d", code, exitUsage)
	}
}

func TestRunInteractiveRootGenerateBuildsThroughToAuth(t *testing.T) {
	// Wizard answers → cmdGenerate → missing API key (no network call needed).
	t.Setenv("OPENAI_API_KEY", "")
	origTerm := stdinIsTerminal
	origLine := readLineFromTerminal
	origSecret := readSecretFromTerminal
	t.Cleanup(func() {
		stdinIsTerminal = origTerm
		readLineFromTerminal = origLine
		readSecretFromTerminal = origSecret
	})
	stdinIsTerminal = func() bool { return true }
	// command, single-line prompt (auto-end), output, quality, size, format
	answers := []string{"generate", "a cat", "x.png", "low", "1024x1024", "png"}
	i := 0
	readLineFromTerminal = func(prompt string) (string, error) {
		if i >= len(answers) {
			t.Fatalf("extra prompt: %q", prompt)
		}
		a := answers[i]
		i++
		return a, nil
	}
	origMore := stdinHasMore
	stdinHasMore = func(time.Duration) bool { return false }
	t.Cleanup(func() { stdinHasMore = origMore })
	// After wizard, resolveAPIKey will try secret prompt because TTY + empty env.
	// Return empty so we get auth exit without calling API.
	readSecretFromTerminal = func() (string, error) { return "", nil }

	code := run([]string{})
	if code != exitAuth {
		t.Fatalf("exit %d want auth %d (used answers %d/%d)", code, exitAuth, i, len(answers))
	}
	if i != len(answers) {
		t.Fatalf("unused answers: %v", answers[i:])
	}
}

func TestPromptChoiceDefault(t *testing.T) {
	withLineAnswers(t, "")
	got, err := promptChoice("Cmd", []string{"generate", "edit"}, "generate")
	if err != nil {
		t.Fatal(err)
	}
	if got != "generate" {
		t.Fatalf("got %q", got)
	}
	_ = strings.TrimSpace
}

func TestLooksLikePastedPrompt(t *testing.T) {
	if looksLikePastedPrompt("generate") {
		t.Fatal("command should not look like paste")
	}
	if !looksLikePastedPrompt("Clean educational infographic visualizing the Creighton Model") {
		t.Fatal("sentence should look like paste")
	}
}

func TestPromptChoiceOptsLongPaste(t *testing.T) {
	withLineAnswers(t, "Clean educational infographic about fertility charting stamps")
	got, err := promptChoiceOpts("Command", []string{"generate", "edit"}, "generate", true)
	if !errors.Is(err, errLongChoice) {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(got, "Clean educational") {
		t.Fatalf("got %q", got)
	}
}

func TestRunInteractiveRootRecoversPasteOnCommand(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	origTerm := stdinIsTerminal
	origLine := readLineFromTerminal
	origSecret := readSecretFromTerminal
	t.Cleanup(func() {
		stdinIsTerminal = origTerm
		readLineFromTerminal = origLine
		readSecretFromTerminal = origSecret
	})
	stdinIsTerminal = func() bool { return true }
	// Paste lands on Command; second paragraph drained as paste; then defaults.
	answers := []string{
		"Clean educational infographic visualizing CrMS",
		"second paragraph of the prompt",
		"", // output default
		"", // quality default
		"", // size default
		"", // format default
	}
	i := 0
	readLineFromTerminal = func(prompt string) (string, error) {
		if i >= len(answers) {
			t.Fatalf("extra prompt: %q", prompt)
		}
		a := answers[i]
		i++
		return a, nil
	}
	// One drained line after the seeded first line.
	left := 1
	origMore := stdinHasMore
	stdinHasMore = func(time.Duration) bool {
		if left > 0 {
			left--
			return true
		}
		return false
	}
	t.Cleanup(func() { stdinHasMore = origMore })
	readSecretFromTerminal = func() (string, error) { return "", nil }
	code := run([]string{})
	if code != exitAuth {
		t.Fatalf("exit %d want auth (got through wizard)", code)
	}
}
