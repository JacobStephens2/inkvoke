package main

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateSize(t *testing.T) {
	cases := []struct {
		name string
		size string
		ok   bool
		want string // substring the message must carry when !ok
	}{
		{"auto", "auto", true, ""},
		{"preset square", "1024x1024", true, ""},
		{"preset landscape", "1536x1024", true, ""},
		{"preset portrait", "1024x1536", true, ""},
		{"free-form multiple of 16", "1280x640", true, ""},
		{"free-form small", "16x16", true, ""},
		{"height not divisible", "1280x648", false, "divisible by 16"},
		{"width not divisible", "1290x640", false, "divisible by 16"},
		{"both not divisible", "1290x648", false, "divisible by 16"},
		{"empty", "", false, "invalid --size"},
		{"no separator", "1280", false, "invalid --size"},
		{"non-numeric", "widexhigh", false, "invalid --size"},
		{"trailing junk", "1280x640px", false, "invalid --size"},
		{"zero", "0x640", false, "invalid --size"},
		{"negative", "-16x640", false, "invalid --size"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSize(tc.size)
			if tc.ok {
				if err != nil {
					t.Fatalf("validateSize(%q) = %v, want nil", tc.size, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateSize(%q) = nil, want error", tc.size)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validateSize(%q) message %q missing %q", tc.size, err, tc.want)
			}
			// Bad geometry is the caller's mistake, not the API's: exit 1, not 3.
			var ue *usageError
			if !errors.As(err, &ue) {
				t.Fatalf("validateSize(%q) = %T, want *usageError (exit %d)", tc.size, err, exitUsage)
			}
			if got := classedOf(err).Exit; got != exitUsage {
				t.Fatalf("validateSize(%q) exit = %d, want %d", tc.size, got, exitUsage)
			}
		})
	}
}

// The documented sizes must themselves be legal, so a copy-pasted example from
// AGENTS.md / README.md never fails. See issue #3.
func TestDocumentedSizesAreValid(t *testing.T) {
	for _, s := range []string{"auto", "1024x1024", "1536x1024", "1024x1536", "1280x640"} {
		if err := validateSize(s); err != nil {
			t.Errorf("documented size %q rejected: %v", s, err)
		}
	}
}

func TestCommonFlagsValidateRejectsBadSize(t *testing.T) {
	c := commonFlags{model: defaultModel, quality: "auto", size: "1280x648", outputFormat: "jpeg", timeoutSec: 300}
	err := c.validate()
	if err == nil || !strings.Contains(err.Error(), "divisible by 16") {
		t.Fatalf("validate() = %v, want a divisible-by-16 usage error", err)
	}
}
