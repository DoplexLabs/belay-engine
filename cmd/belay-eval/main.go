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
	var realClaude bool
	var realCodex bool
	var codexExecutable string
	flag.StringVar(&root, "root", "", "directory for disposable evaluation state")
	flag.StringVar(&output, "output", "", "optional JSON artifact path")
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
	flag.Parse()

	if root == "" {
		value, err := os.MkdirTemp("", "belay-c5-cross-harness-")
		if err != nil {
			fatal(err)
		}
		root = value
	}
	result, err := evalrun.RunCrossHarnessProof(
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
