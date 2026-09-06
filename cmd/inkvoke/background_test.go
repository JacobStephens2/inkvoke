package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBackgroundValidation(t *testing.T) {
	cases := []struct {
		name       string
		background string
		format     string
		wantErr    string
	}{
		{"empty string defaults auto", "", "png", ""},
		{"auto", "auto", "png", ""},
		{"opaque", "opaque", "png", ""},
		{"transparent png", "transparent", "png", ""},
		{"transparent webp", "transparent", "webp", ""},
		{"transparent jpeg", "transparent", "jpeg", "transparent background is not supported with jpeg"},
		{"invalid value", "glass", "png", "invalid --background"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := commonFlags{
				background:   tc.background,
				outputFormat: tc.format,
				model:        defaultModel,
				quality:      "auto",
				size:         "auto",
				timeoutSec:   60,
			}
			err := c.validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("c.validate() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("c.validate() = nil, want error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("c.validate() error = %q, want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestCLIBackgroundValidation(t *testing.T) {
	// Test that run() exits 1 on invalid background or transparent jpeg before any API call
	t.Run("generate transparent jpeg", func(t *testing.T) {
		code := run([]string{"generate", "test prompt", "--background", "transparent", "--output-format", "jpeg"})
		if code != exitUsage {
			t.Fatalf("run() = %d, want exitUsage (%d)", code, exitUsage)
		}
	})

	t.Run("generate invalid background", func(t *testing.T) {
		code := run([]string{"generate", "test prompt", "--background", "invalid"})
		if code != exitUsage {
			t.Fatalf("run() = %d, want exitUsage (%d)", code, exitUsage)
		}
	})

	t.Run("edit transparent jpeg", func(t *testing.T) {
		tmp := filepath.Join(t.TempDir(), "input.png")
		if err := os.WriteFile(tmp, []byte("fake"), 0o644); err != nil {
			t.Fatal(err)
		}
		code := run([]string{"edit", tmp, "--prompt", "test", "--background", "transparent", "--output-format", "jpeg"})
		if code != exitUsage {
			t.Fatalf("run() = %d, want exitUsage (%d)", code, exitUsage)
		}
	})
}

func TestBatchBackground(t *testing.T) {
	manifestContent := `{
		"images": [
			{"id": "img1", "prompt": "a cat", "background": "transparent"},
			{"id": "img2", "prompt": "a dog"}
		]
	}`
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(manifestPath, []byte(manifestContent), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("load manifest background", func(t *testing.T) {
		items, err := loadManifest(manifestPath)
		if err != nil {
			t.Fatalf("loadManifest: %v", err)
		}
		if len(items) != 2 {
			t.Fatalf("got %d items, want 2", len(items))
		}
		if items[0].Background != "transparent" {
			t.Fatalf("item 0 background = %q, want transparent", items[0].Background)
		}
		if items[1].Background != "" {
			t.Fatalf("item 1 background = %q, want empty", items[1].Background)
		}
	})

	t.Run("dry-run inheritance", func(t *testing.T) {
		out := captureStdout(t, func() {
			code := run([]string{"batch", manifestPath, "--dry-run", "--json", "--background", "opaque"})
			if code != exitOK {
				t.Fatalf("run() = %d, want exitOK (%d)", code, exitOK)
			}
		})
		var env resultEnvelope
		if err := json.Unmarshal([]byte(out), &env); err != nil {
			t.Fatalf("json unmarshal: %v\n%s", err, out)
		}
		if len(env.Results) != 2 {
			t.Fatalf("got %d results, want 2", len(env.Results))
		}
		if env.Results[0].Background != "transparent" {
			t.Errorf("result 0 background = %q, want transparent", env.Results[0].Background)
		}
		if env.Results[1].Background != "opaque" {
			t.Errorf("result 1 background = %q, want opaque (inherited)", env.Results[1].Background)
		}
	})

	t.Run("invalid item background", func(t *testing.T) {
		badManifest := filepath.Join(dir, "bad.json")
		_ = os.WriteFile(badManifest, []byte(`[{"id": "bad", "prompt": "p", "background": "invalid"}]`), 0o644)
		code := run([]string{"batch", badManifest, "--dry-run"})
		if code != exitUsage {
			t.Fatalf("run() = %d, want exitUsage", code)
		}
	})

	t.Run("item transparent with jpeg", func(t *testing.T) {
		badManifest := filepath.Join(dir, "bad_jpeg.json")
		_ = os.WriteFile(badManifest, []byte(`[{"id": "bad", "prompt": "p", "background": "transparent"}]`), 0o644)
		code := run([]string{"batch", badManifest, "--dry-run", "--output-format", "jpeg"})
		if code != exitUsage {
			t.Fatalf("run() = %d, want exitUsage", code)
		}
	})
}

func TestClientBackgroundPayload(t *testing.T) {
	var capturedGenerateBody map[string]any
	var capturedEditBackground string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/images/generations" {
			_ = json.NewDecoder(r.Body).Decode(&capturedGenerateBody)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[{"b64_json":"ZmFrZQ=="}]}`))
			return
		}
		if r.URL.Path == "/images/edits" {
			_ = r.ParseMultipartForm(10 << 20)
			capturedEditBackground = r.FormValue("background")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[{"b64_json":"ZmFrZQ=="}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	oldBase := apiBase
	apiBase = srv.URL
	defer func() { apiBase = oldBase }()

	client := NewClient("test-key", 5*time.Second)
	client.SetQuiet(true)

	t.Run("generate sends background", func(t *testing.T) {
		capturedGenerateBody = nil
		opts := RequestOptions{
			Model:        defaultModel,
			Quality:      "auto",
			Size:         "auto",
			OutputFormat: "png",
			Background:   "transparent",
		}
		_, err := client.Generate(context.Background(), "a test prompt", opts)
		if err != nil {
			t.Fatalf("client.Generate failed: %v", err)
		}
		if capturedGenerateBody == nil {
			t.Fatal("expected request body, got nil")
		}
		if capturedGenerateBody["background"] != "transparent" {
			t.Fatalf("request body background = %v, want transparent", capturedGenerateBody["background"])
		}
	})

	t.Run("edit sends background", func(t *testing.T) {
		capturedEditBackground = ""
		tmp := filepath.Join(t.TempDir(), "in.png")
		_ = os.WriteFile(tmp, []byte("fake"), 0o644)

		opts := RequestOptions{
			Model:        defaultModel,
			Quality:      "auto",
			Size:         "auto",
			OutputFormat: "png",
			Background:   "transparent",
		}
		_, err := client.Edit(context.Background(), []string{tmp}, "edit prompt", opts)
		if err != nil {
			t.Fatalf("client.Edit failed: %v", err)
		}
		if capturedEditBackground != "transparent" {
			t.Fatalf("edit form background = %q, want transparent", capturedEditBackground)
		}
	})
}


