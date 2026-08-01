package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveAPIKeyEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "  sk-env-key  ")
	key, err := resolveAPIKey("")
	if err != nil {
		t.Fatal(err)
	}
	if key != "sk-env-key" {
		t.Fatalf("got %q", key)
	}
}

func TestResolveAPIKeyFileOverridesEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "from-env")
	dir := t.TempDir()
	p := filepath.Join(dir, "key.txt")
	if err := os.WriteFile(p, []byte("  from-file \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	key, err := resolveAPIKey(p)
	if err != nil {
		t.Fatal(err)
	}
	if key != "from-file" {
		t.Fatalf("got %q", key)
	}
}

func TestResolveAPIKeyEmptyFile(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "from-env")
	p := filepath.Join(t.TempDir(), "empty.txt")
	if err := os.WriteFile(p, []byte("  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := resolveAPIKey(p)
	if err == nil {
		t.Fatal("expected error for empty key file")
	}
	var ae *authError
	if !errors.As(err, &ae) {
		t.Fatalf("want authError, got %T %v", err, err)
	}
}

func TestResolveAPIKeyMissingNonTTY(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	origTerm := stdinIsTerminal
	origRead := readSecretFromTerminal
	t.Cleanup(func() {
		stdinIsTerminal = origTerm
		readSecretFromTerminal = origRead
	})
	stdinIsTerminal = func() bool { return false }
	readSecretFromTerminal = func() (string, error) {
		t.Fatal("should not prompt when non-TTY")
		return "", nil
	}
	_, err := resolveAPIKey("")
	if err == nil {
		t.Fatal("expected missing key error")
	}
	var ae *authError
	if !errors.As(err, &ae) {
		t.Fatalf("want authError, got %T %v", err, err)
	}
}

func TestResolveAPIKeyInteractivePrompt(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	origTerm := stdinIsTerminal
	origRead := readSecretFromTerminal
	t.Cleanup(func() {
		stdinIsTerminal = origTerm
		readSecretFromTerminal = origRead
	})
	stdinIsTerminal = func() bool { return true }
	readSecretFromTerminal = func() (string, error) {
		return "  sk-prompted  ", nil
	}
	key, err := resolveAPIKey("")
	if err != nil {
		t.Fatal(err)
	}
	if key != "sk-prompted" {
		t.Fatalf("got %q", key)
	}
}

func TestResolveAPIKeyInteractiveEmpty(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	origTerm := stdinIsTerminal
	origRead := readSecretFromTerminal
	t.Cleanup(func() {
		stdinIsTerminal = origTerm
		readSecretFromTerminal = origRead
	})
	stdinIsTerminal = func() bool { return true }
	readSecretFromTerminal = func() (string, error) { return "  ", nil }
	_, err := resolveAPIKey("")
	if err == nil {
		t.Fatal("expected error for empty interactive key")
	}
}
