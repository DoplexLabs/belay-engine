package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/DoplexLabs/belay-engine/internal/acquisition/numbat"
	"github.com/DoplexLabs/belay-engine/internal/localapp"
)

func TestSelectSemanticHarnessHonorsDetectionAndPreference(t *testing.T) {
	bin := t.TempDir()
	for _, name := range []string{"claude", "codex"} {
		path := filepath.Join(bin, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	inventory := numbat.Inventory{
		LaunchTargets: map[numbat.Agent]numbat.InventoryRow{
			numbat.AgentClaude: {
				Agent:    "claude",
				Present:  true,
				Detected: true,
			},
			numbat.AgentCodex: {
				Agent:    "codex",
				Present:  true,
				Detected: true,
			},
		},
	}
	harness, err := selectSemanticHarness(inventory, "auto")
	if err != nil || harness != localapp.SemanticHarnessClaude {
		t.Fatalf("auto harness/error = %q/%v", harness, err)
	}
	harness, err = selectSemanticHarness(inventory, "codex")
	if err != nil || harness != localapp.SemanticHarnessCodex {
		t.Fatalf("Codex harness/error = %q/%v", harness, err)
	}
	if _, err := selectSemanticHarness(inventory, "cursor"); err == nil {
		t.Fatal("unsupported semantic harness was accepted")
	}
}

func TestQuickstartAnalyzeFlagsReachLaunchOptions(t *testing.T) {
	previous := launchLocal
	defer func() { launchLocal = previous }()
	var captured localLaunchOptions
	launchLocal = func(
		_ context.Context,
		options localLaunchOptions,
		_, _ io.Writer,
	) error {
		captured = options
		return nil
	}
	var stdout, stderr bytes.Buffer
	if err := runQuickstart(
		context.Background(),
		[]string{
			"--no-analyze",
			"--analyze-agent",
			"codex",
			"--no-open",
		},
		&stdout,
		&stderr,
	); err != nil {
		t.Fatal(err)
	}
	if captured.analyze ||
		captured.analyzeAgent != "codex" ||
		captured.openBrowser {
		t.Fatalf("quickstart options = %+v", captured)
	}
}
