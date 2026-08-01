package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// resolveSelfBinary returns the absolute path of the running gpt-image binary.
// Symlinks are resolved so uninstall removes the real file when installed via link.
func resolveSelfBinary() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate this binary: %w", err)
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}

func looksLikeEphemeralGoBuild(path string) bool {
	// go run / go test put binaries under the system temp or a go-build cache dir.
	lower := strings.ToLower(filepath.ToSlash(path))
	if strings.Contains(lower, "/go-build/") {
		return true
	}
	base := filepath.Base(path)
	// go test names like "gpt-image.test"
	if strings.HasSuffix(base, ".test") || strings.HasSuffix(base, ".test.exe") {
		return true
	}
	return false
}

// removeSelfBinary deletes the running executable. On Unix this is safe while
// the process is still running; Windows may require elevating or a manual delete.
func removeSelfBinary(path string) error {
	if path == "" {
		return errors.New("empty path")
	}
	if looksLikeEphemeralGoBuild(path) {
		return fmt.Errorf("refusing to uninstall ephemeral build at %s (install a release binary or go install first)", path)
	}
	st, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if st.IsDir() {
		return fmt.Errorf("%s is a directory", path)
	}
	if err := os.Remove(path); err != nil {
		// Common when /usr/local/bin is root-owned.
		return err
	}
	return nil
}

func cmdUninstall(args []string) int {
	const command = "uninstall"
	jsonMode := wantsJSON(args)
	for _, a := range args {
		switch a {
		case "--json", "-json":
			// allowed
		case "-h", "--help", "help":
			fmt.Fprint(os.Stdout, `Usage: gpt-image --uninstall [--json]
       gpt-image uninstall [--json]

Remove the gpt-image binary that is currently running (the install on your PATH).
Does not touch API keys, images, or shell config. May need sudo if the file is
root-owned (e.g. /usr/local/bin/gpt-image).

`)
			return exitOK
		default:
			if strings.HasPrefix(a, "-") {
				return emitFailure(command, usagef("unknown flag %q", a), jsonMode)
			}
			return emitFailure(command, usagef("unexpected argument %q", a), jsonMode)
		}
	}

	path, err := resolveSelfBinary()
	if err != nil {
		return emitFailure(command, iof("%v", err), jsonMode)
	}

	if err := removeSelfBinary(path); err != nil {
		hint := ""
		if runtime.GOOS != "windows" && (os.IsPermission(err) || errors.Is(err, os.ErrPermission)) {
			hint = fmt.Sprintf(" (try: sudo %s --uninstall)", filepath.Base(path))
		}
		if runtime.GOOS == "windows" {
			hint = " (on Windows, close other shells using gpt-image and delete the .exe manually if remove fails)"
		}
		return emitFailure(command, iof("remove %s: %v%s", path, err, hint), jsonMode)
	}

	if jsonMode {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"ok":      true,
			"command": command,
			"removed": path,
		})
		return exitOK
	}
	fmt.Printf("Removed %s\n", path)
	fmt.Fprintln(os.Stderr, "gpt-image is uninstalled from this location. Shell may still hash the old path until you open a new terminal or run: hash -r")
	return exitOK
}
