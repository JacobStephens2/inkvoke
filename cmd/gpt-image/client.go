package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	apiBase         = "https://api.openai.com/v1"
	defaultTimeout  = 200 * time.Second
	retryAttempts   = 3
	retryBaseDelay  = 5 * time.Second
)

// Client talks to OpenAI Images API (generate + edit).
type Client struct {
	apiKey     string
	httpClient *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}
}

// RequestOptions maps to Images API fields shared by generate and edit.
type RequestOptions struct {
	Model        string
	Quality      string
	Size         string
	OutputFormat string
}

// ImageResult is a successful Images API response with at least one b64 image.
type ImageResult struct {
	Raw   map[string]any
	B64   string
	Usage map[string]any
}

func (c *Client) Generate(ctx context.Context, prompt string, opts RequestOptions) (*ImageResult, error) {
	body := map[string]any{
		"model":         opts.Model,
		"prompt":        prompt,
		"quality":       opts.Quality,
		"size":          opts.Size,
		"output_format": opts.OutputFormat,
		"n":             1,
	}
	return c.requestWithRetry(ctx, func(ctx context.Context) (*ImageResult, error) {
		return c.postJSON(ctx, apiBase+"/images/generations", body)
	})
}

func (c *Client) Edit(ctx context.Context, images []string, prompt string, opts RequestOptions) (*ImageResult, error) {
	if len(images) == 0 {
		return nil, fmt.Errorf("edit requires at least one input image")
	}
	return c.requestWithRetry(ctx, func(ctx context.Context) (*ImageResult, error) {
		return c.postEditMultipart(ctx, images, prompt, opts)
	})
}

func (c *Client) requestWithRetry(ctx context.Context, call func(context.Context) (*ImageResult, error)) (*ImageResult, error) {
	var last error
	for attempt := 1; attempt <= retryAttempts; attempt++ {
		result, err := call(ctx)
		if err == nil {
			return result, nil
		}
		last = err
		if attempt == retryAttempts {
			break
		}
		delay := retryBaseDelay * time.Duration(1<<uint(attempt-1))
		fmt.Fprintf(os.Stderr, "  retry %d/%d after error: %v (waiting %s)\n", attempt, retryAttempts-1, err, delay)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	return nil, last
}

func (c *Client) postJSON(ctx context.Context, url string, body map[string]any) (*ImageResult, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return parseImageResponse(resp)
}

func (c *Client) postEditMultipart(ctx context.Context, images []string, prompt string, opts RequestOptions) (*ImageResult, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	// Field names match the OpenAI Python SDK: single file "image", multi "image[]".
	fieldName := "image"
	if len(images) > 1 {
		fieldName = "image[]"
	}
	for _, path := range images {
		if err := writeFilePart(w, fieldName, path); err != nil {
			_ = w.Close()
			return nil, err
		}
	}
	_ = w.WriteField("prompt", prompt)
	_ = w.WriteField("model", opts.Model)
	_ = w.WriteField("quality", opts.Quality)
	_ = w.WriteField("size", opts.Size)
	_ = w.WriteField("output_format", opts.OutputFormat)

	if err := w.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+"/images/edits", &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return parseImageResponse(resp)
}

func writeFilePart(w *multipart.Writer, field, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	part, err := w.CreateFormFile(field, filepath.Base(path))
	if err != nil {
		return err
	}
	_, err = io.Copy(part, f)
	return err
}

func parseImageResponse(resp *http.Response) (*ImageResult, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20)) // 64 MiB cap
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := extractAPIError(body)
		return nil, fmt.Errorf("openai images API HTTP %d: %s", resp.StatusCode, msg)
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	data, _ := raw["data"].([]any)
	if len(data) == 0 {
		return nil, fmt.Errorf("response missing data[0]")
	}
	first, _ := data[0].(map[string]any)
	b64, _ := first["b64_json"].(string)
	if b64 == "" {
		// Some responses may return url only; we require b64 for file write parity with the old CLI.
		return nil, fmt.Errorf("response missing b64_json (set response format to b64 if using a different model)")
	}
	usage, _ := raw["usage"].(map[string]any)
	return &ImageResult{Raw: raw, B64: b64, Usage: usage}, nil
}

func extractAPIError(body []byte) string {
	var wrap struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    any    `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &wrap); err == nil && wrap.Error.Message != "" {
		return wrap.Error.Message
	}
	s := strings.TrimSpace(string(body))
	if len(s) > 400 {
		s = s[:400] + "…"
	}
	if s == "" {
		return "(empty body)"
	}
	return s
}

// WriteImage decodes the first image from result into path (creates parent dirs).
func WriteImage(result *ImageResult, path string) (string, error) {
	if result == nil || result.B64 == "" {
		return "", fmt.Errorf("empty image payload")
	}
	raw, err := base64.StdEncoding.DecodeString(result.B64)
	if err != nil {
		return "", fmt.Errorf("decode b64 image: %w", err)
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return "", err
	}
	return path, nil
}
