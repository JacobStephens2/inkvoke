//go:build unix

package main

import (
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// stdinReadable reports whether stdin has data ready within timeout.
// Used to capture multi-line pastes as one prompt without requiring "---".
func stdinReadable(timeout time.Duration) bool {
	fd := int(os.Stdin.Fd())
	ms := int(timeout / time.Millisecond)
	if ms < 0 {
		ms = 0
	}
	pfd := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	n, err := unix.Poll(pfd, ms)
	if err != nil || n <= 0 {
		return false
	}
	return pfd[0].Revents&unix.POLLIN != 0
}
