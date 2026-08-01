package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLooksLikeEphemeralGoBuild(t *testing.T) {
	if !looksLikeEphemeralGoBuild(filepath.Join(os.TempDir(), "go-build", "b001", "exe")) {
		// path must contain /go-build/
		p := filepath.ToSlash(filepath.Join("/tmp", "go-build", "b001", "exe"))
		if !looksLikeEphemeralGoBuild(p) {
			t.Fatalf("expected go-build path to be ephemeral: %s", p)
		}
	}
	if !looksLikeEphemeralGoBuild("gpt-image.test") {
		t.Fatal("expected .test binary to be ephemeral")
	}
	if looksLikeEphemeralGoBuild("/usr/local/bin/gpt-image") {
		t.Fatal("release path should not be ephemeral")
	}
}

func TestRemoveSelfBinary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gpt-image")
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	if err := os.WriteFile(path, []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := removeSelfBinary(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected file removed, stat err=%v", err)
	}
}

func TestRemoveSelfBinaryRefusesEphemeral(t *testing.T) {
	err := removeSelfBinary(filepath.Join("/tmp", "go-build", "x", "gpt-image"))
	if err == nil || !strings.Contains(err.Error(), "ephemeral") {
		t.Fatalf("got %v", err)
	}
}

func TestCmdUninstallHelp(t *testing.T) {
	code := cmdUninstall([]string{"--help"})
	if code != exitOK {
		t.Fatalf("exit %d", code)
	}
}

func TestCmdUninstallUnknownFlag(t *testing.T) {
	code := cmdUninstall([]string{"--nope"})
	if code != exitUsage {
		t.Fatalf("exit %d want %d", code, exitUsage)
	}
}

func TestRunUninstallFlagRouted(t *testing.T) {
	// --uninstall --help should not try to delete anything.
	code := run([]string{"--uninstall", "--help"})
	if code != exitOK {
		t.Fatalf("exit %d", code)
	}
	code = run([]string{"uninstall", "--help"})
	if code != exitOK {
		t.Fatalf("exit %d", code)
	}
}
