//go:build windows

package main

import "time"

// stdinReadable: on Windows, multi-line paste auto-drain is limited.
// Users can still end multi-line input with a line containing only "---" or Ctrl-Z/EOF.
func stdinReadable(timeout time.Duration) bool {
	_ = timeout
	return false
}
