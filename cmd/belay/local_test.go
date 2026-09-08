package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DoplexLabs/belay-engine/internal/acquisition/numbat"
	"github.com/DoplexLabs/belay-engine/internal/localapp"
)

func TestOnboardLocalHooksReportsPartialFailureAndReturns(t *testing.T) {
	root := t.TempDir()
	client := newLocalHookClient(t, `if [ "$1" = "agents" ]; then
	printf '%s\n' '[{"agent":"codex","present":true,"detected":true},{"agent":"Claude Code","present":true,"detected":true}]'
	exit 0
fi
if [ "$1" = "hook" ] && [ "$4" = "codex" ]; then
	printf '%s\n' 'private-command-output'
	exit 0
fi
printf '%s\n' 'token=private-hook-error' >&2
exit 8`)
	paths := localapp.Paths{
		Root:        root,
		CodexSpool:  filepath.Join(root, "live", "codex.ndjson"),
		ClaudeSpool: filepath.Join(root, "live", "claude.ndjson"),
	}
	var stderr bytes.Buffer

	onboardLocalHooks(context.Background(), client, paths, &stderr)

	output := stderr.String()
	for _, want := range []string{
		`"agent": "codex"`,
		`"agent": "claude"`,
		`"error": "hook operation failed"`,
		"belay local: hook onboarding incomplete; Local remains available",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("onboarding output missing %q:\n%s", want, output)
		}
	}
	for _, prohibited := range []string{"private-command-output", "private-hook-error"} {
		if strings.Contains(output, prohibited) {
			t.Errorf("onboarding output leaked %q:\n%s", prohibited, output)
		}
	}
}

func TestOnboardLocalHooksDiscoveryFailureReturns(t *testing.T) {
	client := newLocalHookClient(t, `printf '%s\n' 'password=private-discovery-error' >&2
exit 7`)
	root := t.TempDir()
	paths := localapp.Paths{
		Root:        root,
		CodexSpool:  filepath.Join(root, "live", "codex.ndjson"),
		ClaudeSpool: filepath.Join(root, "live", "claude.ndjson"),
	}
	var stderr bytes.Buffer

	onboardLocalHooks(context.Background(), client, paths, &stderr)

	output := stderr.String()
	if !strings.Contains(output, "belay local: hook onboarding incomplete; Local remains available") {
		t.Fatalf("onboarding output missing non-fatal warning:\n%s", output)
	}
	if strings.Contains(output, "private-discovery-error") {
		t.Fatalf("onboarding output leaked discovery stderr:\n%s", output)
	}
}

func newLocalHookClient(t *testing.T, body string) *numbat.Client {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "fake-numbat")
	script := fmt.Sprintf("#!/bin/sh\n%s\n", body)
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	client, err := numbat.NewClient(binary)
	if err != nil {
		t.Fatal(err)
	}
	return client
}
