package main

import (
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
	"unicode"

	"github.com/DoplexLabs/belay-engine/internal/acquisition/numbat"
	"github.com/DoplexLabs/belay-engine/internal/analysis"
	"github.com/DoplexLabs/belay-engine/internal/canonical/model"
	"github.com/DoplexLabs/belay-engine/internal/detection"
	"github.com/DoplexLabs/belay-engine/internal/localaction"
	"github.com/DoplexLabs/belay-engine/internal/localapp"
	"github.com/DoplexLabs/belay-engine/internal/presentation/localhttp"
	"github.com/DoplexLabs/belay-engine/internal/presentation/localmcp"
	"github.com/DoplexLabs/belay-engine/internal/presentation/readmodel"
	"github.com/DoplexLabs/belay-engine/internal/storage/local"
)

// Set these at build time for packaged distributions:
//
//	-X main.bundledNumbatSHA256=<lowercase-sha256>
//	-X main.bundledNumbatVersionMarker=<version-output-substring>
var (
	bundledNumbatSHA256        string
	bundledNumbatVersionMarker string
	currentExecutablePath      = os.Executable
)

type localRuntimeFlags struct {
	home            *string
	numbatBinary    *string
	numbatSHA256    *string
	versionMarker   *string
	allowUnverified *bool
}

type preparedRuntime struct {
	paths  localapp.Paths
	config localapp.Config
	client *numbat.Client
}

type localLaunchOptions struct {
	runtime        localRuntimeFlags
	listen         string
	installHooks   bool
	historicalScan bool
	openBrowser    bool
	commandName    string
}

var launchLocal = runLocalLaunch

func addLocalRuntimeFlags(flags *flag.FlagSet) localRuntimeFlags {
	return localRuntimeFlags{
		home:            flags.String("home", "", "Belay Local state directory (default BELAY_HOME or ~/.belay)"),
		numbatBinary:    flags.String("numbat", "", "path to the pinned stock Numbat executable"),
		numbatSHA256:    flags.String("numbat-sha256", "", "expected SHA-256 for the pinned Numbat executable"),
		versionMarker:   flags.String("numbat-version-marker", "", "required substring in `numbat version`"),
		allowUnverified: flags.Bool("allow-unverified-numbat", false, "development only: run Numbat without a configured checksum pin"),
	}
}

func prepareRuntime(ctx context.Context, options localRuntimeFlags) (preparedRuntime, error) {
	paths, err := localapp.ResolvePaths(*options.home)
	if err != nil {
		return preparedRuntime{}, err
	}
	config, err := localapp.LoadOrCreateConfig(paths)
	if err != nil {
		return preparedRuntime{}, err
	}
	changed := false
	if *options.numbatBinary != "" {
		config.NumbatBinary = *options.numbatBinary
		changed = true
	}
	if *options.numbatSHA256 != "" {
		config.NumbatSHA256 = *options.numbatSHA256
		changed = true
	}
	if *options.versionMarker != "" {
		config.NumbatVersionMarker = *options.versionMarker
		changed = true
	}
	if changed {
		if err := localapp.SaveConfig(paths.Config, config); err != nil {
			return preparedRuntime{}, err
		}
	}
	belayExecutable, _ := currentExecutablePath()
	binary, err := localapp.ResolveNumbatBinaryForExecutable(
		paths,
		config,
		*options.numbatBinary,
		belayExecutable,
	)
	if err != nil {
		return preparedRuntime{}, err
	}
	switch {
	case config.NumbatSHA256 == "" && config.NumbatVersionMarker == "":
		if !*options.allowUnverified {
			pin, available, err := compiledNumbatPin()
			if err != nil {
				return preparedRuntime{}, err
			}
			if !available {
				return preparedRuntime{}, errors.New("Numbat is not pinned; provide --numbat-sha256 and --numbat-version-marker, or use --allow-unverified-numbat for development")
			}
			if !isPackagedSiblingNumbat(belayExecutable, binary) {
				return preparedRuntime{}, errors.New("the compiled Numbat pin may bootstrap only the packaged sibling bin/numbat executable")
			}
			verifyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			materialized, verifyErr := localapp.MaterializePinnedNumbat(
				verifyCtx,
				paths,
				binary,
				pin,
			)
			cancel()
			if verifyErr != nil {
				return preparedRuntime{}, verifyErr
			}
			config.NumbatBinary = materialized
			config.NumbatSHA256 = pin.SHA256
			config.NumbatVersionMarker = pin.VersionMarker
			if err := localapp.SaveConfig(paths.Config, config); err != nil {
				return preparedRuntime{}, err
			}
			binary = materialized
		}
	case config.NumbatSHA256 == "" || config.NumbatVersionMarker == "":
		return preparedRuntime{}, errors.New("Numbat pin is incomplete; configure both SHA-256 and version marker")
	default:
		verifyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		binary, err = localapp.MaterializePinnedNumbat(verifyCtx, paths, binary, numbat.BinaryPin{
			SHA256:        config.NumbatSHA256,
			VersionMarker: config.NumbatVersionMarker,
		})
		cancel()
		if err != nil {
			return preparedRuntime{}, err
		}
	}
	client, err := numbat.NewClient(binary)
	if err != nil {
		return preparedRuntime{}, err
	}
	return preparedRuntime{paths: paths, config: config, client: client}, nil
}

func compiledNumbatPin() (numbat.BinaryPin, bool, error) {
	checksum := bundledNumbatSHA256
	marker := bundledNumbatVersionMarker
	if checksum == "" && marker == "" {
		return numbat.BinaryPin{}, false, nil
	}
	if len(checksum) != 64 || marker == "" {
		return numbat.BinaryPin{}, false, errors.New("compiled Numbat pin is invalid")
	}
	decoded, err := hex.DecodeString(checksum)
	if err != nil || hex.EncodeToString(decoded) != checksum {
		return numbat.BinaryPin{}, false, errors.New("compiled Numbat pin is invalid")
	}
	if len(marker) > 256 {
		return numbat.BinaryPin{}, false, errors.New("compiled Numbat pin is invalid")
	}
	for _, character := range marker {
		if unicode.IsControl(character) {
			return numbat.BinaryPin{}, false, errors.New("compiled Numbat pin is invalid")
		}
	}
	return numbat.BinaryPin{SHA256: checksum, VersionMarker: marker}, true, nil
}

func isPackagedSiblingNumbat(belayExecutable, numbatExecutable string) bool {
	belayPath, err := filepath.Abs(belayExecutable)
	if err != nil || filepath.Base(filepath.Dir(belayPath)) != "bin" {
		return false
	}
	numbatPath, err := filepath.Abs(numbatExecutable)
	if err != nil {
		return false
	}
	return filepath.Clean(numbatPath) ==
		filepath.Clean(filepath.Join(filepath.Dir(belayPath), "numbat"))
}

func runLocal(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("local", flag.ContinueOnError)
	flags.SetOutput(stderr)
	runtimeFlags := addLocalRuntimeFlags(flags)
	listen := flags.String("listen", "127.0.0.1:0", "loopback listen address")
	installHooks := flags.Bool("install-hooks", false, "explicitly install monitor-only live hooks for Codex and Claude Code")
	noScan := flags.Bool("no-scan", false, "skip the initial historical scan")
	if err := flags.Parse(args); err != nil {
		return err
	}
	return launchLocal(ctx, localLaunchOptions{
		runtime:        runtimeFlags,
		listen:         *listen,
		installHooks:   *installHooks,
		historicalScan: !*noScan,
		commandName:    "local",
	}, stdout, stderr)
}

func runQuickstart(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("quickstart", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintln(stderr, `usage: belay quickstart [options]

Explicitly initializes private Belay Local state, verifies packaged Numbat,
installs monitor-only hooks for detected Codex and Claude Code installations,
imports minimized local activity, scans local history, and opens a loopback-only
browser. No prompts, completions, file contents, or telemetry are sent to Belay.

Options:`)
		flags.PrintDefaults()
	}
	runtimeFlags := addLocalRuntimeFlags(flags)
	listen := flags.String("listen", "127.0.0.1:0", "loopback listen address")
	noOpen := flags.Bool("no-open", false, "print the Local URL without opening a browser")
	if err := flags.Parse(args); err != nil {
		return err
	}
	return launchLocal(ctx, localLaunchOptions{
		runtime:        runtimeFlags,
		listen:         *listen,
		installHooks:   true,
		historicalScan: true,
		openBrowser:    !*noOpen,
		commandName:    "quickstart",
	}, stdout, stderr)
}

func runLocalLaunch(
	ctx context.Context,
	options localLaunchOptions,
	stdout, stderr io.Writer,
) error {
	runtime, err := prepareRuntime(ctx, options.runtime)
	if err != nil {
		return err
	}
	store, err := local.Open(runtime.paths.Database, local.NewMacOSKeychainProvider())
	if err != nil {
		return err
	}
	defer store.Close()

	if options.installHooks {
		onboardHooks(ctx, runtime.client, runtime.paths, options.commandName, stderr)
	}
	token, err := localhttp.NewLaunchToken()
	if err != nil {
		return err
	}
	localServer, err := newLocalHTTPServer(store, token)
	if err != nil {
		return err
	}
	running, err := localServer.Start(ctx, options.listen)
	if err != nil {
		return err
	}
	startAnalysisRecovery(ctx, store, func() {
		fmt.Fprintf(
			stderr,
			"belay %s: issue analysis recovery pending; Local remains available\n",
			options.commandName,
		)
	})
	if _, err := localapp.ImportLive(ctx, runtime.paths, store, runtime.config); err != nil {
		fmt.Fprintf(stderr, "belay %s: live records will be retried\n", options.commandName)
	}
	go localapp.PollLive(ctx, runtime.paths, store, runtime.config, 2*time.Second, func(error) {
		fmt.Fprintf(stderr, "belay %s: live import retry pending\n", options.commandName)
	})
	if options.historicalScan {
		go func() {
			_, reports, scanErr := localapp.DiscoverAndScan(ctx, runtime.client, store, runtime.config)
			if scanErr != nil {
				fmt.Fprintf(
					stderr,
					"belay %s: historical discovery failed; Local remains available\n",
					options.commandName,
				)
				return
			}
			_ = writeJSON(stderr, reports)
		}()
	}
	browserURL := running.BrowserURL()
	fmt.Fprintln(stdout, browserURL)
	if options.openBrowser {
		attemptBrowserOpen(ctx, browserURL, options.commandName, stderr)
	}
	return running.Wait()
}

func newLocalHTTPServer(store *local.Store, token string) (*localhttp.Server, error) {
	actions, err := localaction.New(store, store)
	if err != nil {
		return nil, err
	}
	return localhttp.New(
		readmodel.New(store, readmodel.WithIssueRepository(store)),
		token,
		localhttp.WithFixService(actions),
	)
}

func onboardLocalHooks(
	ctx context.Context,
	client *numbat.Client,
	paths localapp.Paths,
	stderr io.Writer,
) {
	onboardHooks(ctx, client, paths, "local", stderr)
}

func onboardHooks(
	ctx context.Context,
	client *numbat.Client,
	paths localapp.Paths,
	commandName string,
	stderr io.Writer,
) {
	results, hookErr := localapp.ManageHooks(ctx, client, paths, "install")
	if err := writeJSON(stderr, results); err != nil {
		fmt.Fprintf(
			stderr,
			"belay %s: hook onboarding results could not be reported; Local remains available\n",
			commandName,
		)
	}
	if hookErr != nil {
		fmt.Fprintf(
			stderr,
			"belay %s: hook onboarding incomplete; Local remains available\n",
			commandName,
		)
	}
}

func runScan(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("scan", flag.ContinueOnError)
	flags.SetOutput(stderr)
	runtimeFlags := addLocalRuntimeFlags(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	runtime, err := prepareRuntime(ctx, runtimeFlags)
	if err != nil {
		return err
	}
	store, err := local.Open(runtime.paths.Database, local.NewMacOSKeychainProvider())
	if err != nil {
		return err
	}
	defer store.Close()
	inventory, reports, err := localapp.DiscoverAndScan(ctx, runtime.client, store, runtime.config)
	if writeErr := writeJSON(stdout, map[string]any{
		"inventory": inventory,
		"scans":     reports,
	}); writeErr != nil {
		return writeErr
	}
	return err
}

func runAgents(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("agents", flag.ContinueOnError)
	flags.SetOutput(stderr)
	runtimeFlags := addLocalRuntimeFlags(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	runtime, err := prepareRuntime(ctx, runtimeFlags)
	if err != nil {
		return err
	}
	discoveryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	inventory, _, err := runtime.client.Discover(discoveryCtx)
	if err != nil {
		return err
	}
	return writeJSON(stdout, inventory)
}

func runHooks(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("hooks requires install, status, or uninstall")
	}
	action := args[0]
	if action != "install" && action != "status" && action != "uninstall" {
		return fmt.Errorf("unknown hooks action %q", action)
	}
	flags := flag.NewFlagSet("hooks "+action, flag.ContinueOnError)
	flags.SetOutput(stderr)
	runtimeFlags := addLocalRuntimeFlags(flags)
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	runtime, err := prepareRuntime(ctx, runtimeFlags)
	if err != nil {
		return err
	}
	results, hookErr := localapp.ManageHooks(ctx, runtime.client, runtime.paths, action)
	if err := writeJSON(stdout, results); err != nil {
		return err
	}
	return hookErr
}

func runMCP(ctx context.Context, args []string, stderr io.Writer) error {
	flags := flag.NewFlagSet("mcp", flag.ContinueOnError)
	flags.SetOutput(stderr)
	home := flags.String("home", "", "Belay Local state directory (default BELAY_HOME or ~/.belay)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	paths, err := localapp.ResolvePaths(*home)
	if err != nil {
		return err
	}
	if _, err := localapp.LoadOrCreateConfig(paths); err != nil {
		return err
	}
	store, err := local.Open(paths.Database, local.NewMacOSKeychainProvider())
	if err != nil {
		return err
	}
	defer store.Close()
	server, err := localmcp.New(readmodel.New(store))
	if err != nil {
		return err
	}
	serverDone := make(chan error, 1)
	serverStarted := make(chan struct{})
	go func() {
		close(serverStarted)
		serverDone <- server.RunStdio(ctx)
	}()
	<-serverStarted
	startAnalysisRecovery(ctx, store, func() {
		fmt.Fprintln(stderr, "belay mcp: issue analysis recovery pending")
	})
	return <-serverDone
}

func startAnalysisRecovery(
	ctx context.Context,
	store *local.Store,
	onError func(),
) {
	go func() {
		if _, err := analysis.NewReconciler(store).Startup(ctx); err != nil &&
			onError != nil {
			onError()
		}
	}()
}

func runDoctor(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(stderr)
	runtimeFlags := addLocalRuntimeFlags(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	runtime, err := prepareRuntime(ctx, runtimeFlags)
	if err != nil {
		return err
	}
	store, err := local.Open(runtime.paths.Database, local.NewMacOSKeychainProvider())
	if err != nil {
		return err
	}
	defer store.Close()
	discoveryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	inventory, command, discoverErr := runtime.client.Discover(discoveryCtx)
	analysisStatus, analysisErr := loadDoctorAnalysis(ctx, store)
	status := map[string]any{
		"config":            "ok",
		"encrypted_storage": "ok",
		"numbat_pin":        "ok",
		"inventory":         inventory,
		"detector_catalog":  detection.CatalogVersion,
	}
	if analysisErr != nil {
		status["analysis"] = "failed"
	} else {
		status["analysis"] = analysisStatus
	}
	if discoverErr != nil {
		status["discovery"] = "failed"
		status["numbat_exit_code"] = command.ExitCode
	} else {
		status["discovery"] = "ok"
	}
	if err := writeJSON(stdout, status); err != nil {
		return err
	}
	return errors.Join(discoverErr, analysisErr)
}

type doctorAnalysisStatus struct {
	CatalogVersion string                      `json:"catalog_version"`
	Coverage       model.IssueAnalysisCoverage `json:"coverage"`
}

func loadDoctorAnalysis(
	ctx context.Context,
	store *local.Store,
) (doctorAnalysisStatus, error) {
	page, err := store.QueryIssues(ctx, model.IssueQuery{Limit: 1})
	if err != nil {
		return doctorAnalysisStatus{}, err
	}
	return doctorAnalysisStatus{
		CatalogVersion: detection.CatalogVersion,
		Coverage:       page.Analysis,
	}, nil
}
