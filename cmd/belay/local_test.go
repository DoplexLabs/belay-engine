package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DoplexLabs/belay-engine/internal/acquisition/numbat"
	"github.com/DoplexLabs/belay-engine/internal/detection"
	"github.com/DoplexLabs/belay-engine/internal/localapp"
	"github.com/DoplexLabs/belay-engine/internal/presentation/readmodel"
	"github.com/DoplexLabs/belay-engine/internal/storage/local"
)

func TestPrepareRuntimeBootstrapsVerifiedPackagedSiblingPin(t *testing.T) {
	home := t.TempDir()
	belayExecutable, numbatExecutable, checksum := packagedNumbatFixture(
		t,
		"numbat packaged-marker",
	)
	setCompiledNumbatTestState(t, checksum, "packaged-marker", belayExecutable)
	t.Setenv("BELAY_NUMBAT_BIN", "")

	runtime, err := prepareRuntime(
		context.Background(),
		testRuntimeFlags(home, "", "", "", false),
	)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.config.NumbatSHA256 != checksum ||
		runtime.config.NumbatVersionMarker != "packaged-marker" {
		t.Fatalf("bootstrapped config = %+v", runtime.config)
	}
	if runtime.config.NumbatBinary == numbatExecutable ||
		!strings.HasPrefix(runtime.config.NumbatBinary, runtime.paths.BundledBin+"-") {
		t.Fatalf("materialized binary = %q", runtime.config.NumbatBinary)
	}
	if info, err := os.Stat(runtime.config.NumbatBinary); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("materialized binary is unavailable: %v", err)
	}

	persisted, err := localapp.LoadOrCreateConfig(runtime.paths)
	if err != nil {
		t.Fatal(err)
	}
	if persisted != runtime.config {
		t.Fatalf("persisted config = %+v, want %+v", persisted, runtime.config)
	}
}

func TestPrepareRuntimeRefusesCompiledPinForNonSiblingBinary(t *testing.T) {
	home := t.TempDir()
	belayExecutable, _, _ := packagedNumbatFixture(t, "numbat packaged-marker")
	otherBinary := filepath.Join(t.TempDir(), "numbat")
	checksum := writeVersionedNumbat(t, otherBinary, "numbat packaged-marker")
	setCompiledNumbatTestState(t, checksum, "packaged-marker", belayExecutable)
	t.Setenv("BELAY_NUMBAT_BIN", otherBinary)

	_, err := prepareRuntime(
		context.Background(),
		testRuntimeFlags(home, "", "", "", false),
	)
	if err == nil || !strings.Contains(err.Error(), "packaged sibling") {
		t.Fatalf("prepareRuntime() error = %v, want sibling refusal", err)
	}
	config, loadErr := localapp.LoadOrCreateConfig(mustResolvePaths(t, home))
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if config.NumbatSHA256 != "" || config.NumbatVersionMarker != "" {
		t.Fatalf("failed bootstrap persisted compiled pin: %+v", config)
	}
}

func TestPrepareRuntimeRefusesBadPackagedHashAndVersion(t *testing.T) {
	tests := []struct {
		name   string
		hash   func(string) string
		marker string
	}{
		{
			name: "hash",
			hash: func(string) string {
				return strings.Repeat("0", 64)
			},
			marker: "packaged-marker",
		},
		{
			name:   "version",
			hash:   func(value string) string { return value },
			marker: "wrong-marker",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			belayExecutable, _, checksum := packagedNumbatFixture(
				t,
				"numbat packaged-marker",
			)
			setCompiledNumbatTestState(
				t,
				test.hash(checksum),
				test.marker,
				belayExecutable,
			)
			t.Setenv("BELAY_NUMBAT_BIN", "")

			_, err := prepareRuntime(
				context.Background(),
				testRuntimeFlags(home, "", "", "", false),
			)
			if err == nil {
				t.Fatal("prepareRuntime() succeeded with invalid packaged pin")
			}
			config, loadErr := localapp.LoadOrCreateConfig(mustResolvePaths(t, home))
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if config.NumbatSHA256 != "" || config.NumbatVersionMarker != "" {
				t.Fatalf("failed verification persisted pin: %+v", config)
			}
		})
	}
}

func TestPrepareRuntimePreservesExplicitPinForSourceBuild(t *testing.T) {
	home := t.TempDir()
	binary := filepath.Join(t.TempDir(), "custom-numbat")
	checksum := writeVersionedNumbat(t, binary, "numbat source-marker")
	setCompiledNumbatTestState(
		t,
		"",
		"",
		filepath.Join(t.TempDir(), "bin", "belay"),
	)

	runtime, err := prepareRuntime(
		context.Background(),
		testRuntimeFlags(home, binary, checksum, "source-marker", false),
	)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.config.NumbatSHA256 != checksum ||
		runtime.config.NumbatVersionMarker != "source-marker" {
		t.Fatalf("explicit config = %+v", runtime.config)
	}
}

func TestQuickstartAndLocalLaunchModes(t *testing.T) {
	original := launchLocal
	t.Cleanup(func() { launchLocal = original })
	var captured []localLaunchOptions
	launchLocal = func(
		_ context.Context,
		options localLaunchOptions,
		_, _ io.Writer,
	) error {
		captured = append(captured, options)
		return nil
	}

	var stdout, stderr bytes.Buffer
	if err := run(
		context.Background(),
		[]string{"quickstart"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	); err != nil {
		t.Fatal(err)
	}
	if err := run(
		context.Background(),
		[]string{"quickstart", "--no-open"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	); err != nil {
		t.Fatal(err)
	}
	if err := run(
		context.Background(),
		[]string{"quickstart", "--no-mcp"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	); err != nil {
		t.Fatal(err)
	}
	if err := run(
		context.Background(),
		[]string{"quickstart", "--allow-codex-mcp-add"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	); err != nil {
		t.Fatal(err)
	}
	if err := runLocal(context.Background(), nil, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if err := runLocal(
		context.Background(),
		[]string{"--install-hooks", "--no-scan"},
		&stdout,
		&stderr,
	); err != nil {
		t.Fatal(err)
	}
	if len(captured) != 6 {
		t.Fatalf("captured launches = %d, want 6", len(captured))
	}
	if !captured[0].installHooks || !captured[0].historicalScan ||
		!captured[0].installMCP || !captured[0].openBrowser ||
		captured[0].allowCodexMCPAdd || !captured[0].waitForScan ||
		captured[0].commandName != "quickstart" {
		t.Fatalf("quickstart launch = %+v", captured[0])
	}
	if captured[1].openBrowser || !captured[1].installHooks ||
		!captured[1].installMCP || !captured[1].historicalScan ||
		!captured[1].waitForScan {
		t.Fatalf("quickstart --no-open launch = %+v", captured[1])
	}
	if !captured[2].installHooks || captured[2].installMCP ||
		!captured[2].historicalScan || !captured[2].waitForScan ||
		!captured[2].openBrowser {
		t.Fatalf("quickstart --no-mcp launch = %+v", captured[2])
	}
	if !captured[3].installHooks || !captured[3].installMCP ||
		!captured[3].allowCodexMCPAdd || !captured[3].historicalScan ||
		!captured[3].waitForScan || !captured[3].openBrowser ||
		captured[3].commandName != "quickstart" {
		t.Fatalf("quickstart Codex opt-in launch = %+v", captured[3])
	}
	if captured[4].installHooks || captured[4].installMCP ||
		captured[4].allowCodexMCPAdd || captured[4].openBrowser ||
		!captured[4].historicalScan || captured[4].waitForScan ||
		captured[4].commandName != "local" {
		t.Fatalf("normal local launch changed = %+v", captured[4])
	}
	if !captured[5].installHooks || captured[5].installMCP ||
		captured[5].allowCodexMCPAdd || captured[5].historicalScan ||
		captured[5].waitForScan || captured[5].openBrowser ||
		captured[5].commandName != "local" {
		t.Fatalf("explicit local flags changed = %+v", captured[5])
	}
}

func TestQuickstartCompletesInitialScanBeforeBrowserAndFailsOpen(t *testing.T) {
	previousScan := discoverAndScan
	previousStartServer := startLocalHTTPServer
	previousOpenStore := openLocalCommandStore
	previousOpenBrowser := openBrowser
	t.Cleanup(func() {
		discoverAndScan = previousScan
		startLocalHTTPServer = previousStartServer
		openLocalCommandStore = previousOpenStore
		openBrowser = previousOpenBrowser
	})

	scanStarted := make(chan struct{})
	releaseScan := make(chan struct{})
	discoverAndScan = func(
		context.Context,
		*numbat.Client,
		*local.Store,
		localapp.Config,
	) (numbat.Inventory, []localapp.HarnessScan, error) {
		close(scanStarted)
		<-releaseScan
		return numbat.Inventory{}, nil, errors.New("private scan failure")
	}
	keyProvider := &doctorKeyProvider{keys: make(map[string][]byte)}
	openLocalCommandStore = func(path string) (*local.Store, error) {
		return local.OpenWithOptions(path, local.OpenOptions{KeyProvider: keyProvider})
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	startLocalHTTPServer = func(
		serverCtx context.Context,
		_ *local.Store,
		_ string,
		_ string,
	) (runningLocalServer, error) {
		return fakeRunningLocalServer{
			url: "http://127.0.0.1:12345/#token=test",
			wait: func() error {
				<-serverCtx.Done()
				return nil
			},
		}, nil
	}
	browserOpened := make(chan struct{})
	openBrowser = func(context.Context, string) error {
		close(browserOpened)
		cancel()
		return nil
	}
	binary := filepath.Join(t.TempDir(), "numbat")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- runLocalLaunch(
			ctx,
			localLaunchOptions{
				runtime:        testRuntimeFlags(home, binary, "", "", true),
				listen:         "127.0.0.1:0",
				historicalScan: true,
				waitForScan:    true,
				openBrowser:    true,
				commandName:    "quickstart",
			},
			&stdout,
			&stderr,
		)
	}()

	select {
	case <-scanStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("initial scan did not start")
	}
	select {
	case <-browserOpened:
		t.Fatal("browser opened before initial scan completed")
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseScan)
	select {
	case <-browserOpened:
	case err := <-done:
		t.Fatalf(
			"scan failure prevented browser launch: error=%v stdout=%q stderr=%q",
			err,
			stdout.String(),
			stderr.String(),
		)
	case <-time.After(5 * time.Second):
		t.Fatalf(
			"scan failure delayed fail-open Local launch: stdout=%q stderr=%q",
			stdout.String(),
			stderr.String(),
		)
	}
	if err := <-done; err != nil {
		t.Fatalf("runLocalLaunch() error = %v", err)
	}
	if !strings.Contains(
		stderr.String(),
		"belay quickstart: initial historical scan incomplete; Local will continue",
	) {
		t.Fatalf("quickstart scan warning = %q", stderr.String())
	}
	if strings.Contains(stderr.String(), "private scan failure") {
		t.Fatalf("quickstart scan warning leaked error payload: %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "http://127.0.0.1:") {
		t.Fatalf("quickstart Local URL = %q", stdout.String())
	}
}

type fakeRunningLocalServer struct {
	url  string
	wait func() error
}

func (server fakeRunningLocalServer) BrowserURL() string {
	return server.url
}

func (server fakeRunningLocalServer) Wait() error {
	return server.wait()
}

func TestQuickstartHelpStatesConsentAndPrivacyBoundary(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runQuickstart(
		context.Background(),
		[]string{"--help"},
		&stdout,
		&stderr,
	)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("runQuickstart(--help) error = %v, want flag.ErrHelp", err)
	}
	for _, required := range []string{
		"monitor-only hooks",
		"Codex and Claude Code",
		"loopback-only",
		"No prompts, completions, file contents, or telemetry",
		"read-only user-scoped MCP registration",
		"--no-mcp",
		"--allow-codex-mcp-add",
		"non-atomic",
		"-no-open",
	} {
		if !strings.Contains(stderr.String(), required) {
			t.Fatalf("quickstart help missing %q:\n%s", required, stderr.String())
		}
	}
}

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

func TestQuickstartHookSummaryIsFixedAndPayloadFree(t *testing.T) {
	root := t.TempDir()
	client := newLocalHookClient(t, `if [ "$1" = "agents" ]; then
	printf '%s\n' '[{"agent":"codex","present":true,"detected":true},{"agent":"Claude Code","present":true,"detected":true}]'
	exit 0
fi
if [ "$1" = "hook" ] && [ "$4" = "codex" ]; then
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

	if onboardHooks(
		context.Background(),
		client,
		paths,
		"quickstart",
		&stderr,
	) {
		t.Fatal("quickstart hooks reported complete")
	}
	if got := stderr.String(); got !=
		"belay quickstart: hooks codex=configured claude=failed\n" {
		t.Fatalf("hook summary = %q", got)
	}
	if strings.Contains(stderr.String(), "private-hook-error") {
		t.Fatal("hook summary leaked subprocess output")
	}
}

func TestScanAgentsAndDoctorInventoryProjectionIsPayloadFree(t *testing.T) {
	const inventoryJSON = `[
		{
			"agent":"codex",
			"present":false,
			"detected":true,
			"hook":"PRIVATE_HOOK_CANARY",
			"wired":"PRIVATE_WIRING_CANARY",
			"setup_hint":"/Users/private/setup/PRIVATE_SETUP_HINT_CANARY",
			"future_field":"PRIVATE_UNKNOWN_FIELD_CANARY"
		},
		{
			"agent":"Claude Code",
			"present":false,
			"detected":false,
			"hook":"PRIVATE_CLAUDE_HOOK_CANARY",
			"wired":"PRIVATE_CLAUDE_WIRING_CANARY",
			"setup_hint":"PRIVATE_CLAUDE_SETUP_HINT_CANARY",
			"nested_unknown":{"secret":"PRIVATE_NESTED_UNKNOWN_CANARY"}
		},
		{
			"agent":"future-agent",
			"present":true,
			"detected":true,
			"setup_hint":"PRIVATE_FUTURE_AGENT_CANARY",
			"future_path":"/Users/private/future-agent"
		}
	]`
	binary := writeInventoryNumbat(t, inventoryJSON)
	keyProvider := &doctorKeyProvider{keys: make(map[string][]byte)}
	previousOpen := openLocalCommandStore
	openLocalCommandStore = func(path string) (*local.Store, error) {
		return local.OpenWithOptions(path, local.OpenOptions{KeyProvider: keyProvider})
	}
	t.Cleanup(func() {
		openLocalCommandStore = previousOpen
	})

	commands := []struct {
		name string
		run  func(context.Context, []string, io.Writer, io.Writer) error
	}{
		{name: "scan", run: runScan},
		{name: "agents", run: runAgents},
		{name: "doctor", run: runDoctor},
	}
	for _, command := range commands {
		t.Run(command.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			err := command.run(
				context.Background(),
				[]string{
					"--home", t.TempDir(),
					"--numbat", binary,
					"--allow-unverified-numbat",
				},
				&stdout,
				&stderr,
			)
			if err != nil {
				t.Fatalf("%s error = %v stderr=%s", command.name, err, stderr.String())
			}
			for _, forbidden := range []string{
				"setup_hint",
				"future_field",
				"nested_unknown",
				"PRIVATE_",
				"/Users/private",
				`"hook"`,
				`"wired"`,
				"future-agent",
			} {
				if strings.Contains(stdout.String(), forbidden) {
					t.Fatalf("%s output leaked %q: %s", command.name, forbidden, stdout.String())
				}
			}

			var projected cliInventory
			if command.name == "agents" {
				if err := json.Unmarshal(stdout.Bytes(), &projected); err != nil {
					t.Fatalf("decode agents output: %v", err)
				}
			} else {
				var envelope struct {
					Inventory cliInventory `json:"inventory"`
				}
				if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
					t.Fatalf("decode %s output: %v", command.name, err)
				}
				projected = envelope.Inventory
			}
			if len(projected.Rows) != 2 ||
				projected.Rows[0] != (cliInventoryRow{
					Agent: "codex", Present: false, Detected: true,
				}) ||
				projected.Rows[1] != (cliInventoryRow{
					Agent: "claude", Present: false, Detected: false,
				}) {
				t.Fatalf("%s projected rows = %+v", command.name, projected.Rows)
			}
			if len(projected.LaunchTargets) != 2 {
				t.Fatalf(
					"%s launch target count = %d, want 2",
					command.name,
					len(projected.LaunchTargets),
				)
			}
		})
	}
}

func TestDoctorAnalysisStatusExposesCatalogAndCoverage(t *testing.T) {
	store, err := local.OpenWithOptions(
		filepath.Join(t.TempDir(), "belay.sqlite"),
		local.OpenOptions{
			KeyProvider: &doctorKeyProvider{keys: make(map[string][]byte)},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	status, err := loadDoctorAnalysis(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	if status.CatalogVersion != detection.CatalogVersion ||
		!status.Coverage.Complete ||
		status.Coverage.CurrentSessions != 0 {
		t.Fatalf("doctor analysis status = %+v", status)
	}
}

func TestLocalHTTPWiresFixCapabilityExplicitly(t *testing.T) {
	store, err := local.OpenWithOptions(
		filepath.Join(t.TempDir(), "belay.sqlite"),
		local.OpenOptions{
			KeyProvider: &doctorKeyProvider{keys: make(map[string][]byte)},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	server, err := newLocalHTTPServer(store, "launch-secret")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodGet,
		"http://127.0.0.1/v1/issues/iss_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/fixes",
		nil,
	)
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("Authorization", "Bearer launch-secret")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("fix history status = %d body=%s", response.Code, response.Body.String())
	}
	monitoringRequest := httptest.NewRequest(
		http.MethodGet,
		"http://127.0.0.1/v1/fix-monitoring",
		nil,
	)
	monitoringRequest.RemoteAddr = "127.0.0.1:1234"
	monitoringRequest.Header.Set("Authorization", "Bearer launch-secret")
	monitoringResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(monitoringResponse, monitoringRequest)
	if monitoringResponse.Code != http.StatusServiceUnavailable ||
		!strings.Contains(
			monitoringResponse.Body.String(),
			"belay.local/monitoring-catchup-in-progress",
		) {
		t.Fatalf("monitoring capability status = %d body=%s",
			monitoringResponse.Code, monitoringResponse.Body.String())
	}

	coreOnly := readmodel.New(store)
	if _, err := coreOnly.ListFixMonitoring(
		context.Background(),
		readmodel.FixMonitoringListRequest{},
	); err == nil {
		t.Fatal("core-only readmodel unexpectedly exposes fix monitoring")
	}
}

func TestRunLocalRecoveryOrdersAnalysisCatchupAndPeriodicDrain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls []string
	runLocalRecovery(
		ctx,
		localRecoveryActions{
			analysisStartup: func(context.Context) error {
				calls = append(calls, "analysis")
				return nil
			},
			monitoringStartup: func(context.Context) error {
				calls = append(calls, "monitoring-startup")
				return nil
			},
			monitoringDrain: func(context.Context) error {
				calls = append(calls, "monitoring-drain")
				cancel()
				return nil
			},
		},
		time.Millisecond,
		nil,
		nil,
	)
	if got := strings.Join(calls, ","); got !=
		"analysis,monitoring-startup,monitoring-drain" {
		t.Fatalf("recovery order = %q", got)
	}
}

func TestRunLocalRecoveryRetriesCatchupAndStopsCleanlyOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	analysisStartups := 0
	startups := 0
	warnings := 0
	runLocalRecovery(
		ctx,
		localRecoveryActions{
			analysisStartup: func(context.Context) error {
				analysisStartups++
				if analysisStartups == 1 {
					return errors.New("analysis pending")
				}
				return nil
			},
			monitoringStartup: func(context.Context) error {
				startups++
				if startups == 1 {
					return errors.New("catch-up pending")
				}
				return nil
			},
			monitoringDrain: func(context.Context) error {
				cancel()
				return context.Canceled
			},
		},
		time.Millisecond,
		func() { warnings++ },
		func() { warnings++ },
	)
	if analysisStartups != 2 || startups != 2 || warnings != 2 {
		t.Fatalf(
			"analysis/monitoring attempts/warnings = %d/%d/%d, want 2/2/2",
			analysisStartups,
			startups,
			warnings,
		)
	}

	canceled, stop := context.WithCancel(context.Background())
	stop()
	warnings = 0
	runLocalRecovery(
		canceled,
		localRecoveryActions{
			analysisStartup:   func(ctx context.Context) error { return ctx.Err() },
			monitoringStartup: func(ctx context.Context) error { return ctx.Err() },
		},
		time.Millisecond,
		func() { warnings++ },
		func() { warnings++ },
	)
	if warnings != 0 {
		t.Fatalf("cancellation emitted %d recovery warnings", warnings)
	}
}

func TestRunLocalRecoveryBoundsRepeatedDrainWarnings(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	drains := 0
	warnings := 0
	runLocalRecovery(
		ctx,
		localRecoveryActions{
			analysisStartup:   func(context.Context) error { return nil },
			monitoringStartup: func(context.Context) error { return nil },
			monitoringDrain: func(context.Context) error {
				drains++
				switch drains {
				case 1, 2, 4:
					return errors.New("retryable drain failure")
				case 3:
					return nil
				default:
					cancel()
					return context.Canceled
				}
			},
		},
		time.Millisecond,
		nil,
		func() { warnings++ },
	)
	if drains != 5 || warnings != 2 {
		t.Fatalf("drains/warnings = %d/%d, want 5/2", drains, warnings)
	}
}

type doctorKeyProvider struct {
	keys map[string][]byte
}

func (provider *doctorKeyProvider) Load(
	_ context.Context,
	storeID string,
) ([]byte, error) {
	key, ok := provider.keys[storeID]
	if !ok {
		return nil, local.ErrKeyNotFound
	}
	return append([]byte(nil), key...), nil
}

func (provider *doctorKeyProvider) Create(
	_ context.Context,
	storeID string,
) ([]byte, error) {
	key := bytes.Repeat([]byte{0x72}, 32)
	provider.keys[storeID] = key
	return append([]byte(nil), key...), nil
}

func testRuntimeFlags(
	home string,
	binary string,
	checksum string,
	marker string,
	allowUnverified bool,
) localRuntimeFlags {
	return localRuntimeFlags{
		home:            &home,
		numbatBinary:    &binary,
		numbatSHA256:    &checksum,
		versionMarker:   &marker,
		allowUnverified: &allowUnverified,
	}
}

func setCompiledNumbatTestState(
	t *testing.T,
	checksum string,
	marker string,
	executable string,
) {
	t.Helper()
	oldChecksum := bundledNumbatSHA256
	oldMarker := bundledNumbatVersionMarker
	oldExecutable := currentExecutablePath
	bundledNumbatSHA256 = checksum
	bundledNumbatVersionMarker = marker
	currentExecutablePath = func() (string, error) { return executable, nil }
	t.Cleanup(func() {
		bundledNumbatSHA256 = oldChecksum
		bundledNumbatVersionMarker = oldMarker
		currentExecutablePath = oldExecutable
	})
}

func packagedNumbatFixture(t *testing.T, versionOutput string) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	belayExecutable := filepath.Join(bin, "belay")
	numbatExecutable := filepath.Join(bin, "numbat")
	checksum := writeVersionedNumbat(t, numbatExecutable, versionOutput)
	return belayExecutable, numbatExecutable, checksum
}

func writeVersionedNumbat(t *testing.T, path, versionOutput string) string {
	t.Helper()
	body := []byte(fmt.Sprintf(`#!/bin/sh
if [ "$1" = "version" ]; then
	printf '%%s\n' %q
	exit 0
fi
exit 0
`, versionOutput))
	if err := os.WriteFile(path, body, 0o700); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	return fmt.Sprintf("%x", sum[:])
}

func mustResolvePaths(t *testing.T, root string) localapp.Paths {
	t.Helper()
	paths, err := localapp.ResolvePaths(root)
	if err != nil {
		t.Fatal(err)
	}
	return paths
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

func writeInventoryNumbat(t *testing.T, inventory string) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "fake-numbat-inventory")
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "agents" ]; then
	printf '%%b\n' %q
	exit 0
fi
exit 2
`, inventory)
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return binary
}
