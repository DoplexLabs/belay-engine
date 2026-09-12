// Command belay-eval runs isolated Belay product-value evaluations.
//
// It is intentionally not part of the end-user Belay command surface.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/DoplexLabs/belay-engine/internal/evalrun"
)

func main() {
	var root string
	var output string
	var comparative bool
	var privateEvalCapsule bool
	var capsuleDatabase string
	var capsuleProposal string
	var realClaude bool
	var realCodex bool
	var codexExecutable string
	var model string
	var repetitions int
	flag.StringVar(&root, "root", "", "directory for disposable evaluation state")
	flag.StringVar(&output, "output", "", "optional JSON artifact path")
	flag.BoolVar(
		&comparative,
		"comparative",
		false,
		"run the five-baseline C6 comparative pilot",
	)
	flag.BoolVar(
		&privateEvalCapsule,
		"private-eval-capsule",
		false,
		"export an evidence-bound private eval capsule from a disposable store copy",
	)
	flag.StringVar(
		&capsuleDatabase,
		"capsule-db",
		"",
		"path to a disposable Belay SQLite copy",
	)
	flag.StringVar(
		&capsuleProposal,
		"capsule-proposal",
		"",
		"current semantic proposal ID to bind into the capsule",
	)
	flag.BoolVar(
		&realClaude,
		"real-claude",
		false,
		"invoke installed Claude headlessly for semantic proposal generation",
	)
	flag.BoolVar(
		&realCodex,
		"real-codex",
		false,
		"invoke installed Codex ephemerally for the destination session",
	)
	flag.StringVar(
		&codexExecutable,
		"codex",
		"codex",
		"Codex executable name or path",
	)
	flag.StringVar(
		&model,
		"model",
		"openai.gpt-5.6-sol",
		"fixed Codex model for comparative evaluation",
	)
	flag.IntVar(
		&repetitions,
		"repetitions",
		1,
		"paired comparative repetitions (1-5)",
	)
	flag.Parse()

	if root == "" && !privateEvalCapsule {
		value, err := os.MkdirTemp("", "belay-c5-cross-harness-")
		if err != nil {
			fatal(err)
		}
		root = value
	}
	var result any
	if privateEvalCapsule {
		if comparative || realClaude || realCodex {
			fatal(fmt.Errorf(
				"private eval capsule export cannot be combined with harness evaluation modes",
			))
		}
		value, err := evalrun.LoadPrivateEvalCapsule(
			context.Background(),
			capsuleDatabase,
			capsuleProposal,
		)
		if err != nil {
			fatal(err)
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
			fatal(err)
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
			fatal(err)
		}
		result = value
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fatal(err)
	}
	data = append(data, '\n')
	if output != "" {
		path, err := filepath.Abs(output)
		if err != nil {
			fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			fatal(err)
		}
		fmt.Println(path)
		return
	}
	if _, err := os.Stdout.Write(data); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "belay-eval:", err)
	os.Exit(1)
}
