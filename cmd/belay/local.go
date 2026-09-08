package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/DoplexLabs/belay-engine/internal/acquisition/numbat"
	"github.com/DoplexLabs/belay-engine/internal/localapp"
	"github.com/DoplexLabs/belay-engine/internal/presentation/localhttp"
	"github.com/DoplexLabs/belay-engine/internal/presentation/localmcp"
	"github.com/DoplexLabs/belay-engine/internal/presentation/readmodel"
	"github.com/DoplexLabs/belay-engine/internal/storage/local"
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
	binary, err := localapp.ResolveNumbatBinary(paths, config, *options.numbatBinary)
	if err != nil {
		return preparedRuntime{}, err
	}
	if config.NumbatSHA256 == "" || config.NumbatVersionMarker == "" {
		if !*options.allowUnverified {
			return preparedRuntime{}, errors.New("Numbat is not pinned; provide --numbat-sha256 and --numbat-version-marker, or use --allow-unverified-numbat for development")
		}
	} else {
		verifyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		binary, err = localapp.MaterializePinnedNumbat(verifyCtx, paths, binary, numbat.BinaryPin{
			SHA256:        config.NumbatSHA256,
			VersionMarker: config.NumbatVersionMarker,
		})
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
	runtime, err := prepareRuntime(ctx, runtimeFlags)
	if err != nil {
		return err
	}
	store, err := local.Open(runtime.paths.Database, local.NewMacOSKeychainProvider())
	if err != nil {
		return err
	}
	defer store.Close()

	if _, err := localapp.ImportLive(ctx, runtime.paths, store, runtime.config); err != nil {
		fmt.Fprintln(stderr, "belay local: live records will be retried")
	}
	if *installHooks {
		results, hookErr := localapp.ManageHooks(ctx, runtime.client, runtime.paths, "install")
		if err := writeJSON(stderr, results); err != nil {
			return err
		}
		if hookErr != nil {
			return hookErr
		}
	}
	token, err := localhttp.NewLaunchToken()
	if err != nil {
		return err
	}
	localServer, err := localhttp.New(readmodel.New(store), token)
	if err != nil {
		return err
	}
	running, err := localServer.Start(ctx, *listen)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, running.BrowserURL())

	go localapp.PollLive(ctx, runtime.paths, store, runtime.config, 2*time.Second, func(error) {
		fmt.Fprintln(stderr, "belay local: live import retry pending")
	})
	if !*noScan {
		go func() {
			_, reports, scanErr := localapp.DiscoverAndScan(ctx, runtime.client, store, runtime.config)
			if scanErr != nil {
				fmt.Fprintln(stderr, "belay local: historical discovery failed; Local remains available")
				return
			}
			_ = writeJSON(stderr, reports)
		}()
	}
	return running.Wait()
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
	return server.RunStdio(ctx)
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
	status := map[string]any{
		"config":            "ok",
		"encrypted_storage": "ok",
		"numbat_pin":        "ok",
		"inventory":         inventory,
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
	return discoverErr
}
