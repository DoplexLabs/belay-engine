package localapp

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func TestManageHooksInstallSkipsAbsentHarnesses(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, "calls")
	client := newHookRuntimeClient(t, logPath,
		`[{"agent":"codex","present":true,"detected":false},{"agent":"Claude Code","present":false,"detected":false}]`,
		"exit 0",
	)
	paths := hookRuntimePaths(root)

	results, err := ManageHooks(context.Background(), client, paths, "install")
	if err != nil {
		t.Fatalf("ManageHooks() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("ManageHooks() results = %+v, want none", results)
	}
	if got, want := readHookRuntimeLog(t, logPath), "<agents><--format><json>\n"; got != want {
		t.Fatalf("Numbat calls = %q, want %q", got, want)
	}
	for _, spool := range []string{paths.CodexSpool, paths.ClaudeSpool} {
		if _, err := os.Stat(spool); !os.IsNotExist(err) {
			t.Fatalf("absent harness spool %q exists or stat failed: %v", spool, err)
		}
	}
}

func TestManageHooksInstallOnlyDetectedHarness(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, "calls")
	client := newHookRuntimeClient(t, logPath,
		`[{"agent":"codex","present":true,"detected":true},{"agent":"Claude Code","present":true,"detected":false}]`,
		"exit 0",
	)
	paths := hookRuntimePaths(root)

	results, err := ManageHooks(context.Background(), client, paths, "install")
	if err != nil {
		t.Fatalf("ManageHooks() error = %v", err)
	}
	if len(results) != 1 || results[0].Agent != "codex" || results[0].Error != "" || results[0].ExitCode != 0 {
		t.Fatalf("ManageHooks() results = %+v, want one successful Codex install", results)
	}
	wantCalls := strings.Join([]string{
		"<agents><--format><json>",
		"<hook><install><--agent><codex><--emit><all><--output><file><--output-file><" + paths.CodexSpool + ">",
		"",
	}, "\n")
	if got := readHookRuntimeLog(t, logPath); got != wantCalls {
		t.Fatalf("Numbat calls:\n%s\nwant:\n%s", got, wantCalls)
	}
	if _, err := os.Stat(paths.CodexSpool); err != nil {
		t.Fatalf("Codex spool was not prepared: %v", err)
	}
	if _, err := os.Stat(paths.ClaudeSpool); !os.IsNotExist(err) {
		t.Fatalf("Claude spool exists or stat failed: %v", err)
	}
}

func TestManageHooksInstallReturnsPartialFailure(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, "calls")
	client := newHookRuntimeClient(t, logPath,
		`[{"agent":"codex","present":true,"detected":true},{"agent":"Claude Code","present":true,"detected":true}]`,
		`if [ "$1" = "hook" ] && [ "$4" = "claude" ]; then
	printf '%s\n' 'token=do-not-report' >&2
	exit 9
fi
exit 0`,
	)
	paths := hookRuntimePaths(root)

	results, err := ManageHooks(context.Background(), client, paths, "install")
	if err == nil {
		t.Fatal("ManageHooks() error = nil, want partial failure")
	}
	if len(results) != 2 {
		t.Fatalf("ManageHooks() result count = %d, want 2", len(results))
	}
	if results[0].Agent != "codex" || results[0].Error != "" || results[0].ExitCode != 0 {
		t.Fatalf("Codex result = %+v, want success", results[0])
	}
	if results[1].Agent != "claude" || results[1].Error != "hook operation failed" || results[1].ExitCode != 9 {
		t.Fatalf("Claude result = %+v, want generic failure", results[1])
	}
	if strings.Contains(err.Error(), "do-not-report") {
		t.Fatalf("ManageHooks() error leaked command stderr: %v", err)
	}
}

func TestManageHooksStatusAndUninstallDoNotRequireDetection(t *testing.T) {
	for _, action := range []string{"status", "uninstall"} {
		t.Run(action, func(t *testing.T) {
			root := t.TempDir()
			logPath := filepath.Join(root, "calls")
			client := newHookRuntimeClient(t, logPath, `[]`, "exit 0")
			paths := hookRuntimePaths(root)

			results, err := ManageHooks(context.Background(), client, paths, action)
			if err != nil {
				t.Fatalf("ManageHooks() error = %v", err)
			}
			if len(results) != 2 {
				t.Fatalf("ManageHooks() result count = %d, want 2", len(results))
			}
			calls := readHookRuntimeLog(t, logPath)
			if strings.Contains(calls, "<agents>") {
				t.Fatalf("ManageHooks(%q) unexpectedly discovered inventory: %s", action, calls)
			}
			for _, agent := range []string{"codex", "claude"} {
				want := "<hook><" + action + "><--agent><" + agent + ">"
				if !strings.Contains(calls, want) {
					t.Errorf("ManageHooks(%q) calls missing %q: %s", action, want, calls)
				}
			}
		})
	}
}

func hookRuntimePaths(root string) Paths {
	return Paths{
		Root:        root,
		CodexSpool:  filepath.Join(root, "live", "codex.ndjson"),
		ClaudeSpool: filepath.Join(root, "live", "claude.ndjson"),
	}
}

func newHookRuntimeClient(t *testing.T, logPath, inventory, hookBody string) *numbat.Client {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "fake-numbat")
	script := fmt.Sprintf(`#!/bin/sh
{
	for arg in "$@"; do
		printf '<%%s>' "$arg"
	done
	printf '\n'
} >> %q
if [ "$1" = "agents" ]; then
	printf '%%s\n' %q
	exit 0
fi
%s
`, logPath, inventory, hookBody)
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	client, err := numbat.NewClient(binary)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func readHookRuntimeLog(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
