package main

import (
	"log/slog"
	"strings"
	"testing"
)

func TestParseLogLevel(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		input   string
		want    slog.Level
		wantErr bool
	}{
		{name: "default empty", input: "", want: slog.LevelInfo},
		{name: "debug", input: "debug", want: slog.LevelDebug},
		{name: "info", input: "info", want: slog.LevelInfo},
		{name: "warn", input: "warn", want: slog.LevelWarn},
		{name: "warning alias", input: "warning", want: slog.LevelWarn},
		{name: "error", input: "error", want: slog.LevelError},
		{name: "trimmed uppercase", input: "  DEBUG  ", want: slog.LevelDebug},
		{name: "invalid", input: "trace", wantErr: true},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseLogLevel(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseLogLevel(%q) expected error", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseLogLevel(%q) error = %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("parseLogLevel(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestRootCmdRejectsInvalidLogLevel(t *testing.T) {
	t.Parallel()

	_, stderr, err := executeCLI(t, "--log-level", "trace", "version")
	if err == nil {
		t.Fatalf("expected invalid --log-level to fail")
	}
	if !strings.Contains(err.Error(), "invalid --log-level") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stderr, "invalid --log-level") {
		t.Fatalf("stderr should include invalid --log-level, got %q", stderr)
	}
}
