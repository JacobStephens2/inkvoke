package main

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteFilePartMIMEHeaders(t *testing.T) {
	cases := []struct {
		filename    string
		wantType    string
	}{
		{"photo.png", "image/png"},
		{"photo.PNG", "image/png"},
		{"photo.jpg", "image/jpeg"},
		{"photo.jpeg", "image/jpeg"},
		{"photo.webp", "image/webp"},
		{"photo.bin", "application/octet-stream"},
	}

	dir := t.TempDir()
	for _, tc := range cases {
		t.Run(tc.filename, func(t *testing.T) {
			filePath := filepath.Join(dir, tc.filename)
			if err := os.WriteFile(filePath, []byte("fake-image-bytes"), 0o644); err != nil {
				t.Fatalf("failed to write test file: %v", err)
			}

			var buf bytes.Buffer
			w := multipart.NewWriter(&buf)
			if err := writeFilePart(w, "image", filePath); err != nil {
				t.Fatalf("writeFilePart failed: %v", err)
			}
			if err := w.Close(); err != nil {
				t.Fatalf("writer.Close failed: %v", err)
			}

			reader := multipart.NewReader(&buf, w.Boundary())
			part, err := reader.NextPart()
			if err != nil {
				t.Fatalf("NextPart failed: %v", err)
			}

			gotType := part.Header.Get("Content-Type")
			if gotType != tc.wantType {
				t.Errorf("Content-Type = %q, want %q", gotType, tc.wantType)
			}
		})
	}
}

func TestClientEditMultipartUpload(t *testing.T) {
	var capturedContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/images/edits" {
			err := r.ParseMultipartForm(10 << 20)
			if err != nil {
				t.Errorf("ParseMultipartForm error: %v", err)
			}
			if files := r.MultipartForm.File["image"]; len(files) > 0 {
				capturedContentType = files[0].Header.Get("Content-Type")
			}
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

	tmp := filepath.Join(t.TempDir(), "input.png")
	if err := os.WriteFile(tmp, []byte("fake-png-bytes"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	client := NewClient("test-key", 5*time.Second)
	client.SetQuiet(true)

	opts := RequestOptions{
		Model:        defaultModel,
		Quality:      "auto",
		Size:         "auto",
		OutputFormat: "png",
	}
	_, err := client.Edit(context.Background(), []string{tmp}, "edit prompt", opts)
	if err != nil {
		t.Fatalf("client.Edit failed: %v", err)
	}

	if capturedContentType != "image/png" {
		t.Errorf("uploaded file Content-Type = %q, want image/png", capturedContentType)
	}
}
