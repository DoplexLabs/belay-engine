// Command belay-eval runs isolated Belay product-value evaluations.
//
// It is intentionally not part of the end-user Belay command surface.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/DoplexLabs/belay-engine/internal/evalrun"
)

const maxCapsuleJSONBytes = 4 << 20

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fatal(err)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	var root string
	var output string
	var comparative bool
	var privateEvalCapsule bool
	var completePrivateEvalCapsule bool
	var capsuleDatabase string
	var capsuleProposal string
	var capsuleDraft string
	var capsuleReplay string
	var realClaude bool
	var realCodex bool
	var codexExecutable string
	var model string
	var repetitions int
	flags := flag.NewFlagSet("belay-eval", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&root, "root", "", "directory for disposable evaluation state")
	flags.StringVar(&output, "output", "", "optional JSON artifact path")
	flags.BoolVar(
		&comparative,
		"comparative",
		false,
		"run the five-baseline C6 comparative pilot",
	)
	flags.BoolVar(
		&privateEvalCapsule,
		"private-eval-capsule",
		false,
		"export an evidence-bound private eval capsule from a disposable store copy",
	)
	flags.BoolVar(
		&completePrivateEvalCapsule,
		"complete-private-eval-capsule",
		false,
		"complete a draft private eval capsule from engineering JSON inputs",
	)
	flags.StringVar(
		&capsuleDatabase,
		"capsule-db",
		"",
		"path to a disposable Belay SQLite copy",
	)
	flags.StringVar(
		&capsuleProposal,
		"capsule-proposal",
		"",
		"current semantic proposal ID to bind into the capsule",
	)
	flags.StringVar(
		&capsuleDraft,
		"capsule-draft",
		"",
		"path to a draft private eval capsule JSON file",
	)
	flags.StringVar(
		&capsuleReplay,
		"capsule-replay",
		"",
		"path to a private eval replay contract JSON file",
	)
	flags.BoolVar(
		&realClaude,
		"real-claude",
		false,
		"invoke installed Claude headlessly for semantic proposal generation",
	)
	flags.BoolVar(
		&realCodex,
		"real-codex",
		false,
		"invoke installed Codex ephemerally for the destination session",
	)
	flags.StringVar(
		&codexExecutable,
		"codex",
		"codex",
		"Codex executable name or path",
	)
	flags.StringVar(
		&model,
		"model",
		"openai.gpt-5.6-sol",
		"fixed Codex model for comparative evaluation",
	)
	flags.IntVar(
		&repetitions,
		"repetitions",
		1,
		"paired comparative repetitions (1-5)",
	)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("belay-eval accepts no positional arguments")
	}

	if completePrivateEvalCapsule {
		if privateEvalCapsule || comparative || realClaude || realCodex {
			return errors.New(
				"private eval capsule completion cannot be combined with other evaluation modes",
			)
		}
		if strings.TrimSpace(capsuleDatabase) != "" ||
			strings.TrimSpace(capsuleProposal) != "" {
			return errors.New(
				"private eval capsule completion cannot be combined with capsule export inputs",
			)
		}
	} else if strings.TrimSpace(capsuleDraft) != "" ||
		strings.TrimSpace(capsuleReplay) != "" {
		return errors.New(
			"--capsule-draft and --capsule-replay require --complete-private-eval-capsule",
		)
	}

	if root == "" && !privateEvalCapsule && !completePrivateEvalCapsule {
		value, err := os.MkdirTemp("", "belay-c5-cross-harness-")
		if err != nil {
			return err
		}
		root = value
	}
	var result any
	if completePrivateEvalCapsule {
		value, err := completeCapsuleFromFiles(capsuleDraft, capsuleReplay)
		if err != nil {
			return err
		}
		result = value
	} else if privateEvalCapsule {
		if comparative || realClaude || realCodex {
			return fmt.Errorf(
				"private eval capsule export cannot be combined with harness evaluation modes",
			)
		}
		value, err := evalrun.LoadPrivateEvalCapsule(
			context.Background(),
			capsuleDatabase,
			capsuleProposal,
		)
		if err != nil {
			return err
		}
		result = value
	} else if comparative {
		value, err := evalrun.RunComparativePilot(
			context.Background(),
			root,
			evalrun.ComparativeOptions{
				CodexExecutable: codexExecutable,
				Model:           model,
				Repetitions:     repetitions,
			},
		)
		if err != nil {
			return err
		}
		result = value
	} else {
		value, err := evalrun.RunCrossHarnessProof(
			context.Background(),
			root,
			evalrun.CrossHarnessOptions{
				RealClaude:      realClaude,
				RealCodex:       realCodex,
				CodexExecutable: codexExecutable,
			},
		)
		if err != nil {
			return err
		}
		result = value
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if output != "" {
		path, err := filepath.Abs(output)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, path)
		return err
	}
	_, err = stdout.Write(data)
	return err
}

func completeCapsuleFromFiles(
	draftPath string,
	replayPath string,
) (evalrun.PrivateEvalCapsule, error) {
	if strings.TrimSpace(draftPath) == "" ||
		strings.TrimSpace(replayPath) == "" {
		return evalrun.PrivateEvalCapsule{}, errors.New(
			"--capsule-draft and --capsule-replay are required",
		)
	}
	var draft evalrun.PrivateEvalCapsule
	if err := decodeStrictJSONFile(
		draftPath,
		"private eval capsule draft",
		&draft,
	); err != nil {
		return evalrun.PrivateEvalCapsule{}, err
	}
	var replay evalrun.CapsuleReplayContract
	if err := decodeStrictJSONFile(
		replayPath,
		"private eval capsule replay",
		&replay,
	); err != nil {
		return evalrun.PrivateEvalCapsule{}, err
	}
	return evalrun.CompletePrivateEvalCapsule(draft, replay)
}

func decodeStrictJSONFile(path, label string, target any) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", label, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s must be a regular file", label)
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", label, err)
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, maxCapsuleJSONBytes+1))
	if err != nil {
		return fmt.Errorf("read %s: %w", label, err)
	}
	if len(body) > maxCapsuleJSONBytes {
		return fmt.Errorf("%s exceeds the size limit", label)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode %s: %w", label, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("%s contains a trailing JSON value", label)
		}
		return fmt.Errorf("%s contains trailing JSON: %w", label, err)
	}
	return nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "belay-eval:", err)
	os.Exit(1)
}
