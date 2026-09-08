package local

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const keychainService = "dev.doplex.belay.local.data-key.v1"
const keychainCommandTimeout = 5 * time.Second

type securityCommandResult struct {
	stdout   []byte
	exitCode int
	err      error
}

type securityCommandRunner interface {
	run(ctx context.Context, stdin io.Reader, args ...string) securityCommandResult
}

type execSecurityCommandRunner struct{}

func (execSecurityCommandRunner) run(ctx context.Context, stdin io.Reader, args ...string) securityCommandResult {
	command := exec.CommandContext(ctx, "/usr/bin/security", args...)
	command.Stdin = stdin
	var stdout bytes.Buffer
	command.Stdout = &stdout
	var stderr bytes.Buffer
	command.Stderr = &stderr
	err := command.Run()
	result := securityCommandResult{stdout: stdout.Bytes(), err: err}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		result.exitCode = exitError.ExitCode()
	}
	return result
}

// MacOSKeychainProvider stores one Local data key per persisted random store
// ID. It invokes /usr/bin/security directly without a shell.
type MacOSKeychainProvider struct {
	runner  securityCommandRunner
	random  io.Reader
	goos    string
	timeout time.Duration
}

func NewMacOSKeychainProvider() *MacOSKeychainProvider {
	return &MacOSKeychainProvider{
		runner:  execSecurityCommandRunner{},
		random:  rand.Reader,
		goos:    runtime.GOOS,
		timeout: keychainCommandTimeout,
	}
}

func (p *MacOSKeychainProvider) Load(ctx context.Context, storeID string) ([]byte, error) {
	if p.goos != "darwin" {
		return nil, errors.New("macOS Keychain is unavailable on this platform")
	}
	if storeID == "" {
		return nil, errors.New("local store ID is required")
	}
	commandContext, cancel := p.commandContext(ctx)
	defer cancel()
	result := p.runner.run(
		commandContext,
		nil,
		"find-generic-password",
		"-a", storeID,
		"-s", keychainService,
		"-w",
	)
	if result.err != nil {
		if errors.Is(commandContext.Err(), context.DeadlineExceeded) {
			return nil, errors.New("macOS Keychain command timed out")
		}
		if result.exitCode == 44 {
			return nil, ErrKeyNotFound
		}
		return nil, errors.New("load local data key from macOS Keychain")
	}
	encoded := strings.TrimSpace(string(result.stdout))
	key, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil || len(key) != 32 {
		return nil, errors.New("decode local data key from macOS Keychain")
	}
	return key, nil
}

func (p *MacOSKeychainProvider) Create(ctx context.Context, storeID string) ([]byte, error) {
	if p.goos != "darwin" {
		return nil, errors.New("macOS Keychain is unavailable on this platform")
	}
	if storeID == "" {
		return nil, errors.New("local store ID is required")
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(p.random, key); err != nil {
		return nil, errors.New("generate local data key")
	}
	encoded := base64.RawStdEncoding.EncodeToString(key)
	commandContext, cancel := p.commandContext(ctx)
	defer cancel()
	result := p.runner.run(
		commandContext,
		strings.NewReader(encoded+"\n"),
		"add-generic-password",
		"-a", storeID,
		"-s", keychainService,
		"-l", "Belay Local encrypted store "+storeID,
		"-w",
	)
	if result.err != nil {
		for index := range key {
			key[index] = 0
		}
		if errors.Is(commandContext.Err(), context.DeadlineExceeded) {
			return nil, errors.New("macOS Keychain command timed out")
		}
		if result.exitCode == 45 {
			return nil, ErrKeyAlreadyExists
		}
		return nil, errors.New("store local data key in macOS Keychain")
	}
	return key, nil
}

func (p *MacOSKeychainProvider) commandContext(parent context.Context) (context.Context, context.CancelFunc) {
	timeout := p.timeout
	if timeout <= 0 {
		timeout = keychainCommandTimeout
	}
	return context.WithTimeout(parent, timeout)
}
