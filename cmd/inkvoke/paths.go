package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// maxPathComponent is a conservative limit under typical NAME_MAX (255).
const maxPathComponent = 200

// maxPathLen rejects absurd paths (often leftover prompt prose from paste).
const maxPathLen = 240

func looksLikeBadOutputPath(p string) bool {
	p = strings.TrimSpace(p)
	if p == "" {
		return false // empty means "use default"
	}
	if len(p) > maxPathLen {
		return true
	}
	base := filepath.Base(p)
	if utf8.RuneCountInString(base) > maxPathComponent {
		return true
	}
	// Long prose without path separators is almost never intentional.
	if !strings.ContainsAny(p, `/\`) && strings.Count(p, " ") >= 4 && len(p) > 48 {
		return true
	}
	// Shell scaffolding pasted by mistake.
	if strings.Contains(p, "<<") || strings.Contains(p, "EOF") {
		return true
	}
	return false
}

// resolveOutputPath returns a usable path. If the user-supplied path is unusable,
// falls back to def (or a timestamped name) and explains on stderr when interactive.
func resolveOutputPath(user, def, format string, warn bool) string {
	user = strings.TrimSpace(user)
	if user == "" {
		user = def
	}
	if user == "" {
		user = "generated." + defaultExt(format)
	}
	if looksLikeBadOutputPath(user) {
		fallback := def
		if fallback == "" || looksLikeBadOutputPath(fallback) {
			fallback = fmt.Sprintf("inkvoke-%s.%s", time.Now().Format("20060102-150405"), defaultExt(format))
		}
		if warn {
			preview := user
			if len(preview) > 60 {
				preview = preview[:57] + "..."
			}
			fmt.Fprintf(os.Stderr, "note: output path %q looks invalid (too long or not a path); using %s\n", preview, fallback)
		}
		return fallback
	}
	return user
}

func defaultExt(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "jpeg", "jpg":
		return "jpg"
	case "webp":
		return "webp"
	default:
		return "png"
	}
}

// writeImageResult writes the image to path, or to a safe fallback if that fails.
// Always returns an absolute path on success so the user knows exactly where it went.
func writeImageResult(result *ImageResult, path, format string) (string, error) {
	path = expandHome(strings.TrimSpace(path))
	if path == "" || looksLikeBadOutputPath(path) {
		path = fmt.Sprintf("inkvoke-%s.%s", time.Now().Format("20060102-150405"), defaultExt(format))
	}

	written, err := WriteImage(result, path)
	if err == nil {
		return absPath(written), nil
	}

	fallback := fmt.Sprintf("inkvoke-%s.%s", time.Now().Format("20060102-150405"), defaultExt(format))
	// Avoid colliding with the failed path if it was already a timestamp name.
	if filepath.Clean(path) == filepath.Clean(fallback) {
		fallback = fmt.Sprintf("inkvoke-save-%s.%s", time.Now().Format("20060102-150405.000"), defaultExt(format))
	}
	written2, err2 := WriteImage(result, fallback)
	if err2 != nil {
		return "", fmt.Errorf("could not write %q: %v; also failed writing fallback %q: %v", path, err, fallback, err2)
	}
	abs := absPath(written2)
	fmt.Fprintf(os.Stderr, "warning: could not write %q (%v)\n", path, err)
	fmt.Fprintf(os.Stderr, "saved image to: %s\n", abs)
	return abs, nil
}

// normalizeInteractivePrompt cleans pasted shell wrappers so only the image
// description is sent to the API.
func normalizeInteractivePrompt(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	// Drop common "copy the whole CLI example" prefixes.
	prefixes := []string{
		`gpt-image generate "$(cat <<'EOF'`,
		`gpt-image generate "$(cat <<EOF`,
		`inkvoke generate "$(cat <<'EOF'`,
		`inkvoke generate "$(cat <<EOF`,
		"gpt-image generate <<'EOF'",
		"inkvoke generate <<'EOF'",
		`gpt-image generate "`,
		`inkvoke generate "`,
	}
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			s = strings.TrimSpace(strings.TrimPrefix(s, p))
			break
		}
	}
	// Strip trailing heredoc / quote closers.
	if i := strings.LastIndex(s, "\nEOF"); i >= 0 {
		tail := strings.TrimSpace(s[i+1:])
		if tail == "EOF" || strings.HasPrefix(tail, "EOF") {
			s = strings.TrimSpace(s[:i])
		}
	}
	s = strings.TrimSpace(s)
	for _, suf := range []string{`)"`, `EOF)"`, `"`, "EOF"} {
		if strings.HasSuffix(s, suf) && len(s) > len(suf)+10 {
			// only strip EOF-like closers, not a prompt that legitimately ends with a quote mid-sentence
			if suf == `"` && strings.Count(s, `"`)%2 == 0 {
				continue
			}
			if suf == "EOF" || strings.HasSuffix(s, "\n"+suf) || strings.HasSuffix(s, suf) && strings.Contains(s, "EOF") {
				s = strings.TrimSpace(strings.TrimSuffix(s, suf))
			}
		}
	}
	return strings.TrimSpace(s)
}
