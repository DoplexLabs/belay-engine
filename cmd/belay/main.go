package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/DoplexLabs/belay-engine/internal/acquisition/numbat"
	"github.com/DoplexLabs/belay-engine/internal/pipeline"
	"github.com/DoplexLabs/belay-engine/internal/presentation/readmodel"
	"github.com/DoplexLabs/belay-engine/internal/storage/local"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "belay:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printUsage(stderr)
		return errors.New("missing command")
	}
	switch args[0] {
	case "import":
		return runImport(ctx, args[1:], stdin, stdout, stderr)
	case "sessions":
		return runSessions(ctx, args[1:], stdout, stderr)
	case "timeline":
		return runTimeline(ctx, args[1:], stdout, stderr)
	case "verify-numbat":
		return runVerifyNumbat(ctx, args[1:], stderr)
	case "help", "-h", "--help":
		printUsage(stdout)
		return nil
	default:
		printUsage(stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runImport(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("import", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dbPath := flags.String("db", "", "path to the Belay Local SQLite database")
	inputPath := flags.String("input", "-", "Numbat NDJSON file, or - for stdin")
	installationID := flags.String("installation-id", "", "random Belay installation ID")
	engineVersion := flags.String("engine-version", numbat.ResearchCommit, "pinned Numbat release or research commit")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *dbPath == "" || *installationID == "" {
		return errors.New("--db and --installation-id are required")
	}
	input := stdin
	var file *os.File
	if *inputPath != "-" {
		var err error
		file, err = os.Open(*inputPath)
		if err != nil {
			return err
		}
		defer file.Close()
		input = file
	}
	store, err := local.Open(*dbPath)
	if err != nil {
		return err
	}
	defer store.Close()
	report, err := pipeline.New(store, *installationID, *engineVersion).Import(ctx, input)
	if err != nil {
		return err
	}
	return writeJSON(stdout, report)
}

func runSessions(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("sessions", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dbPath := flags.String("db", "", "path to the Belay Local SQLite database")
	limit := flags.Int("limit", 20, "maximum sessions")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *dbPath == "" {
		return errors.New("--db is required")
	}
	store, err := local.Open(*dbPath)
	if err != nil {
		return err
	}
	defer store.Close()
	response, err := readmodel.New(store).ListSessions(ctx, *limit)
	if err != nil {
		return err
	}
	return writeJSON(stdout, response)
}

func runTimeline(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("timeline", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dbPath := flags.String("db", "", "path to the Belay Local SQLite database")
	sessionID := flags.String("session", "", "Belay session ID")
	limit := flags.Int("limit", 100, "maximum events")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *dbPath == "" || *sessionID == "" {
		return errors.New("--db and --session are required")
	}
	store, err := local.Open(*dbPath)
	if err != nil {
		return err
	}
	defer store.Close()
	response, err := readmodel.New(store).GetSessionTimeline(ctx, *sessionID, *limit)
	if err != nil {
		return err
	}
	return writeJSON(stdout, response)
}

func runVerifyNumbat(ctx context.Context, args []string, stderr io.Writer) error {
	flags := flag.NewFlagSet("verify-numbat", flag.ContinueOnError)
	flags.SetOutput(stderr)
	binary := flags.String("binary", "", "path to the stock Numbat binary")
	checksum := flags.String("sha256", "", "expected lowercase SHA-256")
	version := flags.String("version-marker", "", "required substring in numbat version output")
	timeout := flags.Duration("timeout", 5*time.Second, "version command timeout")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *binary == "" || *checksum == "" || *version == "" {
		return errors.New("--binary, --sha256, and --version-marker are required")
	}
	verifyCtx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	return numbat.VerifyBinary(verifyCtx, *binary, numbat.BinaryPin{
		SHA256:        *checksum,
		VersionMarker: *version,
	})
}

func writeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func printUsage(writer io.Writer) {
	fmt.Fprintln(writer, `usage: belay COMMAND

Commands:
  import          import strict Numbat 0.3.0 NDJSON into Belay Local
  sessions        list Local session summaries
  timeline        get one Local session timeline
  verify-numbat   verify a pinned Numbat binary checksum and version marker`)
}
