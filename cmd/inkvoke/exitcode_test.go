package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"testing"
)

func TestClassedOf(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		exit      int
		code      string
		retryable bool
	}{
		{"nil", nil, exitOK, "ok", false},
		{"help", flag.ErrHelp, exitOK, "ok", false},
		{"usage", usagef("bad flag"), exitUsage, "usage", false},
		{"auth", authf("missing API key"), exitAuth, "auth", false},
		{"io", iof("write failed"), exitIO, "io", false},
		{"api 400", &APIError{StatusCode: 400, Message: "bad request"}, exitAPIPermanent, "api_permanent", false},
		{"api 401", &APIError{StatusCode: 401, Message: "nope"}, exitAuth, "auth", false},
		{"api 429", &APIError{StatusCode: 429, Message: "slow down"}, exitAPIRetryable, "api_retryable", true},
		{"api 500", &APIError{StatusCode: 500, Message: "boom"}, exitAPIRetryable, "api_retryable", true},
		{"deadline", context.DeadlineExceeded, exitAPIRetryable, "timeout", true},
		{"wrapped auth string", errors.New("missing API key: set OPENAI_API_KEY"), exitAuth, "auth", false},
		{"not exist", os.ErrNotExist, exitIO, "io", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := classedOf(tc.err)
			if c.Exit != tc.exit || c.Code != tc.code || c.Retryable != tc.retryable {
				t.Fatalf("got exit=%d code=%q retry=%v want exit=%d code=%q retry=%v (msg=%q)",
					c.Exit, c.Code, c.Retryable, tc.exit, tc.code, tc.retryable, c.Message)
			}
		})
	}
}

func TestAPIErrorRetryable(t *testing.T) {
	if (&APIError{StatusCode: 400}).Retryable() {
		t.Fatal("400 should not be retryable")
	}
	if !(&APIError{StatusCode: 429}).Retryable() {
		t.Fatal("429 should be retryable")
	}
	if !(&APIError{StatusCode: 503}).Retryable() {
		t.Fatal("503 should be retryable")
	}
}

func TestIsPermanentAPIError(t *testing.T) {
	if !isPermanentAPIError(&APIError{StatusCode: 400, Message: "x"}) {
		t.Fatal("400 should be permanent")
	}
	if isPermanentAPIError(&APIError{StatusCode: 429, Message: "x"}) {
		t.Fatal("429 should not be permanent")
	}
	if isPermanentAPIError(errors.New("random")) {
		t.Fatal("unknown should not match typed permanent check")
	}
}
