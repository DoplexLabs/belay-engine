package numbatmap

import "testing"

func TestSafeCommandSummaryAllowlist(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		command     string
		wantName    string
		wantSummary string
	}{
		{
			name:        "safe executable and option names",
			command:     "go test ./... --count=1 -race --token=secret",
			wantName:    "go",
			wantSummary: "go --count -race",
		},
		{
			name:        "separated option value omitted",
			command:     "rg --glob *.go needle",
			wantName:    "",
			wantSummary: "",
		},
		{
			name:    "environment prefix",
			command: "TOKEN=secret go test ./...",
		},
		{
			name:    "shell wrapper",
			command: "bash -c go test ./...",
		},
		{
			name:    "absolute executable",
			command: "/usr/bin/go test ./...",
		},
		{
			name:    "unknown executable",
			command: "private-tool --help",
		},
		{
			name:    "pipe",
			command: "go test ./... | cat",
		},
		{
			name:    "redirect",
			command: "go test ./... > result.txt",
		},
		{
			name:    "substitution",
			command: "go test $(cat target)",
		},
		{
			name:    "control character",
			command: "go test\n./...",
		},
		{
			name:        "unsafe option families omitted",
			command:     "go test -Dsecret --password=secret --help",
			wantName:    "go",
			wantSummary: "go --help",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := commandName(test.command); got != test.wantName {
				t.Fatalf("commandName(%q) = %q, want %q", test.command, got, test.wantName)
			}
			if got := commandSummary(test.command); got != test.wantSummary {
				t.Fatalf(
					"commandSummary(%q) = %q, want %q",
					test.command,
					got,
					test.wantSummary,
				)
			}
		})
	}
}
