package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSummarizeUsage(t *testing.T) {
	result := &ImageResult{
		Usage: map[string]any{
			"input_tokens":  float64(18),
			"output_tokens": float64(1760),
			"input_tokens_details": map[string]any{
				"text_tokens":  float64(18),
				"image_tokens": float64(0),
			},
			"output_tokens_details": map[string]any{
				"image_tokens": float64(1760),
				"text_tokens":  float64(0),
			},
		},
	}
	u := SummarizeUsage(result, "gpt-image-2")
	if u.InputTokens != 18 || u.OutputTokens != 1760 {
		t.Fatalf("tokens: in=%d out=%d", u.InputTokens, u.OutputTokens)
	}
	if u.CostUSD == nil {
		t.Fatal("expected cost")
	}
	want := (18*5.0 + 1760*30.0) / 1_000_000
	if *u.CostUSD < want-1e-9 || *u.CostUSD > want+1e-9 {
		t.Fatalf("cost got %v want %v", *u.CostUSD, want)
	}
	if u.FormatLine(41*time.Second) == "" {
		t.Fatal("empty format line")
	}
}

func TestLoadManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "m.json")
	content := `[
	  {"id": "cover", "prompt": "a stone house"},
	  {"prompt": "make the door red", "edit_from": "inputs/house.png"}
	]`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	items, err := loadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("len=%d", len(items))
	}
	if items[1].ID != "image-02" {
		t.Fatalf("default id: %q", items[1].ID)
	}
	sources, err := parseEditFrom(items[1].EditFrom)
	if err != nil || len(sources) != 1 {
		t.Fatalf("edit_from: %v %v", sources, err)
	}
}
