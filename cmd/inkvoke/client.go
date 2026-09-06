package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var apiBase = "https://api.openai.com/v1"

const (
	defaultTimeout = 300 * time.Second // high-quality gpt-image-2 often needs 1–3+ minutes
	retryAttempts  = 3
	retryBaseDelay = 5 * time.Second
	maxBodyBytes   = 96 << 20 // 96 MiB: high-res b64 PNG payloads can be large
	progressEvery  = 10 * time.Second
)

// APIError is a non-2xx Images API response. Classification (auth / permanent /
// retryable) is done at the CLI exit-code seam so callers do not scrape strings.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("openai images API HTTP %d: %s", e.StatusCode, e.Message)
}

// Retryable reports whether a later attempt is worth making (timeouts, rate
// limits, and 5xx). 4xx other than 408/429 are permanent.
func (e *APIError) Retryable() bool {
	if e == nil {
		return false
	}
	if e.StatusCode == 408 || e.StatusCode == 429 {
		return true
	}
	return e.StatusCode >= 500 && e.StatusCode <= 599
}

// Client talks to OpenAI Images API (generate + edit).
type Client struct {
	apiKey     string
	httpClient *http.Client
	quiet      bool // suppress progress heartbeats on stderr
}

// NewClient builds an Images API client. timeout <= 0 uses defaultTimeout (5m).
func NewClient(apiKey string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: timeout,
			// Transport defaults are fine; do not set ResponseHeaderTimeout alone —
			// image generation can take minutes before the first byte.
		},
	}
}

// SetQuiet disables stderr progress heartbeats during long API calls.
func (c *Client) SetQuiet(q bool) { c.quiet = q }

// RequestOptions maps to Images API fields shared by generate and edit.
type RequestOptions struct {
	Model        string
	Quality      string
	Size         string
	OutputFormat string
	Background   string
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
	if opts.Background != "" {
		body["background"] = opts.Background
	}
	return c.requestWithRetry(ctx, "generate", func(ctx context.Context) (*ImageResult, error) {
		return c.postJSON(ctx, apiBase+"/images/generations", body)
	})
}

func (c *Client) Edit(ctx context.Context, images []string, prompt string, opts RequestOptions) (*ImageResult, error) {
	if len(images) == 0 {
		return nil, fmt.Errorf("edit requires at least one input image")
	}
	return c.requestWithRetry(ctx, "edit", func(ctx context.Context) (*ImageResult, error) {
		return c.postEditMultipart(ctx, images, prompt, opts)
	})
}

func (c *Client) requestWithRetry(ctx context.Context, kind string, call func(context.Context) (*ImageResult, error)) (*ImageResult, error) {
	var last error
	for attempt := 1; attempt <= retryAttempts; attempt++ {
		if !c.quiet {
			if attempt == 1 {
				fmt.Fprintf(os.Stderr, "calling OpenAI images API (%s)…\n", kind)
			} else {
				fmt.Fprintf(os.Stderr, "calling OpenAI images API (%s, attempt %d/%d)…\n", kind, attempt, retryAttempts)
			}
		}
		result, err := c.withProgress(ctx, call)
		if err == nil {
			return result, nil
		}
		last = err
		if attempt == retryAttempts {
			break
		}
		// Do not retry clearly permanent client errors.
		if isPermanentAPIError(err) {
			return nil, err
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

// withProgress prints elapsed-time heartbeats on stderr while call runs.
// High-quality generations often sit silent for 30–120s; without this the CLI looks hung.
func (c *Client) withProgress(ctx context.Context, call func(context.Context) (*ImageResult, error)) (*ImageResult, error) {
	if c.quiet {
		return call(ctx)
	}
	done := make(chan struct{})
	start := time.Now()
	go func() {
		t := time.NewTicker(progressEvery)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				fmt.Fprintf(os.Stderr, "  … still waiting on OpenAI (%ds)\n", int(time.Since(start).Seconds()))
			}
		}
	}()
	result, err := call(ctx)
	close(done)
	return result, err
}

func isPermanentAPIError(err error) bool {
	var api *APIError
	if errors.As(err, &api) {
		return !api.Retryable()
	}
	return false
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
	if opts.Background != "" {
		_ = w.WriteField("background", opts.Background)
	}

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
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{StatusCode: resp.StatusCode, Message: extractAPIError(body)}
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
