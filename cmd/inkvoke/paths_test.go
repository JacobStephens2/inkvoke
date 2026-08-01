package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLooksLikeBadOutputPath(t *testing.T) {
	if looksLikeBadOutputPath("generated.png") {
		t.Fatal("good path")
	}
	if looksLikeBadOutputPath("out/foo.png") {
		t.Fatal("good nested path")
	}
	prose := `Footer note in small type: "Learn with a FertilityCare Practitioner · charting tool only." No anatomy close-ups`
	if !looksLikeBadOutputPath(prose) {
		t.Fatal("expected prose to be rejected")
	}
	long := strings.Repeat("a", 300) + ".png"
	if !looksLikeBadOutputPath(long) {
		t.Fatal("expected long name rejected")
	}
}

func TestResolveOutputPathFallback(t *testing.T) {
	prose := "Footer note in small type Learn with a FertilityCare Practitioner charting tool only"
	got := resolveOutputPath(prose, "generated.png", "png", false)
	if got != "generated.png" {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizeInteractivePromptStripsShellWrapper(t *testing.T) {
	raw := "gpt-image generate \"$(cat <<'EOF'\n" +
		"a lighthouse in a storm\n" +
		"EOF\n)\""
	got := normalizeInteractivePrompt(raw)
	if !strings.Contains(got, "lighthouse") {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, "gpt-image generate") || strings.Contains(got, "EOF") {
		t.Fatalf("wrapper not stripped: %q", got)
	}
}

func TestWriteImageResultFallback(t *testing.T) {
	dir := t.TempDir()
	// Chdir so fallback lands in temp dir.
	wd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	// Minimal 1x1 PNG in base64.
	const tiny = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	result := &ImageResult{B64: tiny}
	bad := strings.Repeat("x", 300) + ".png"
	path, err := writeImageResult(result, bad, "png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("missing %s: %v", path, err)
	}
	if !strings.Contains(filepath.Base(path), "inkvoke-") {
		t.Fatalf("expected fallback name, got %s", path)
	}
}
