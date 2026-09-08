package localapp

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/DoplexLabs/belay-engine/internal/acquisition/numbat"
	"github.com/DoplexLabs/belay-engine/internal/storage/local"
)

type memoryKeyProvider struct {
	keys map[string][]byte
}

func (p *memoryKeyProvider) Load(_ context.Context, storeID string) ([]byte, error) {
	key, ok := p.keys[storeID]
	if !ok {
		return nil, local.ErrKeyNotFound
	}
	return append([]byte(nil), key...), nil
}

func (p *memoryKeyProvider) Create(_ context.Context, storeID string) ([]byte, error) {
	if _, ok := p.keys[storeID]; ok {
		return nil, local.ErrKeyAlreadyExists
	}
	key := bytes.Repeat([]byte{0x42}, 32)
	p.keys[storeID] = key
	return append([]byte(nil), key...), nil
}

func TestDiscoverAndScanImportsCodexAndClaude(t *testing.T) {
	root := t.TempDir()
	codexFixture, err := filepath.Abs(filepath.Join("..", "..", "testdata", "numbat", "v0.3.0", "live", "codex-sanitized.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	claudeFixture, err := filepath.Abs(filepath.Join("..", "..", "testdata", "numbat", "v0.3.0", "live", "claude-sanitized.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(root, "fake-numbat")
	script := fmt.Sprintf(`#!/bin/sh
case "$1" in
  agents)
    printf '%%s\n' '[{"agent":"Codex","present":true,"detected":true},{"agent":"Claude Code","present":true,"detected":true}]'
    ;;
  scan)
    if [ "$3" = "codex" ]; then
      /bin/cat %q
    else
      /bin/cat %q
    fi
    ;;
  *)
    exit 2
    ;;
esac
`, codexFixture, claudeFixture)
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	client, err := numbat.NewClient(binary)
	if err != nil {
		t.Fatal(err)
	}
	store, err := local.OpenWithOptions(filepath.Join(root, "belay.sqlite"), local.OpenOptions{
		KeyProvider: &memoryKeyProvider{keys: make(map[string][]byte)},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	inventory, reports, err := DiscoverAndScan(context.Background(), client, store, Config{
		Version:             ConfigVersion,
		InstallationID:      "inst_runtime_test",
		NumbatVersionMarker: "numbat-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.LaunchTargets) != 2 || len(reports) != 2 {
		t.Fatalf("inventory/reports = %d/%d, want 2/2", len(inventory.LaunchTargets), len(reports))
	}
	for _, report := range reports {
		if report.Error != "" || report.ExitCode != 0 || report.Import.EventsAccepted == 0 {
			t.Fatalf("scan report = %+v", report)
		}
	}
	sessions, _, err := store.ListSessions(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(sessions))
	}
}
