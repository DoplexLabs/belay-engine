package local

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestMacOSKeychainProviderLoadAndCreateWithoutRealKeychain(t *testing.T) {
	key := bytes.Repeat([]byte{0x7b}, 32)
	runner := &recordingSecurityRunner{
		results: []securityCommandResult{
			{stdout: []byte(base64.RawStdEncoding.EncodeToString(key) + "\n")},
			{},
		},
	}
	provider := &MacOSKeychainProvider{
		runner: runner,
		random: bytes.NewReader(key),
		goos:   "darwin",
	}

	loaded, err := provider.Load(context.Background(), "store_test")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !bytes.Equal(loaded, key) {
		t.Fatalf("Load() returned unexpected key")
	}
	created, err := provider.Create(context.Background(), "store_test")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !bytes.Equal(created, key) {
		t.Fatalf("Create() returned unexpected key")
	}
	if len(runner.calls) != 2 {
		t.Fatalf("security calls = %d, want 2", len(runner.calls))
	}
	if !slices.Equal(runner.calls[0].args, []string{
		"find-generic-password",
		"-a", "store_test",
		"-s", keychainService,
		"-w",
	}) {
		t.Fatalf("Load() arguments = %q", runner.calls[0].args)
	}
	if slices.Contains(runner.calls[1].args, base64.RawStdEncoding.EncodeToString(key)) {
		t.Fatal("Create() exposed key material in process arguments")
	}
	if !slices.Equal(runner.calls[1].args[len(runner.calls[1].args)-1:], []string{"-w"}) {
		t.Fatalf("Create() must place prompt-only -w last: %q", runner.calls[1].args)
	}
	wantStdin := base64.RawStdEncoding.EncodeToString(key) + "\n"
	if runner.calls[1].stdin != wantStdin {
		t.Fatalf("Create() stdin = %q, want encoded key", runner.calls[1].stdin)
	}
}

func TestMacOSKeychainProviderErrorsArePayloadFree(t *testing.T) {
	const secretCanary = "KEYCHAIN_ERROR_SECRET_CANARY"
	provider := &MacOSKeychainProvider{
		runner: &recordingSecurityRunner{
			results: []securityCommandResult{
				{stdout: []byte(secretCanary), exitCode: 44, err: errors.New(secretCanary)},
				{stdout: []byte(secretCanary), exitCode: 1, err: errors.New(secretCanary)},
				{stdout: []byte(secretCanary), exitCode: 1, err: errors.New(secretCanary)},
			},
		},
		random: bytes.NewReader(bytes.Repeat([]byte{0x41}, 32)),
		goos:   "darwin",
	}
	if _, err := provider.Load(context.Background(), "store_missing"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("Load() error = %v, want ErrKeyNotFound", err)
	}
	if _, err := provider.Load(context.Background(), "store_error"); err == nil {
		t.Fatal("Load() generic error unexpectedly succeeded")
	} else if strings.Contains(err.Error(), secretCanary) {
		t.Fatalf("Load() leaked command output in error: %v", err)
	}
	if _, err := provider.Create(context.Background(), "store_error"); err == nil {
		t.Fatal("Create() unexpectedly succeeded")
	} else if strings.Contains(err.Error(), secretCanary) {
		t.Fatalf("Create() leaked command output in error: %v", err)
	}
}

func TestMacOSKeychainProviderCommandsHaveBoundedTimeout(t *testing.T) {
	provider := &MacOSKeychainProvider{
		runner:  blockingSecurityRunner{},
		random:  bytes.NewReader(bytes.Repeat([]byte{0x41}, 32)),
		goos:    "darwin",
		timeout: 10 * time.Millisecond,
	}
	started := time.Now()
	if _, err := provider.Load(context.Background(), "store_timeout"); err == nil {
		t.Fatal("Load() timeout unexpectedly succeeded")
	} else if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("Load() timeout error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("Keychain timeout took %s, want bounded execution", elapsed)
	}
}

type securityCall struct {
	args  []string
	stdin string
}

type recordingSecurityRunner struct {
	results []securityCommandResult
	calls   []securityCall
}

type blockingSecurityRunner struct{}

func (blockingSecurityRunner) run(
	ctx context.Context,
	_ io.Reader,
	_ ...string,
) securityCommandResult {
	<-ctx.Done()
	return securityCommandResult{err: ctx.Err()}
}

func (runner *recordingSecurityRunner) run(
	_ context.Context,
	stdin io.Reader,
	args ...string,
) securityCommandResult {
	var body []byte
	if stdin != nil {
		body, _ = io.ReadAll(stdin)
	}
	runner.calls = append(runner.calls, securityCall{
		args:  append([]string(nil), args...),
		stdin: string(body),
	})
	if len(runner.results) == 0 {
		return securityCommandResult{err: errors.New("unexpected security invocation")}
	}
	result := runner.results[0]
	runner.results = runner.results[1:]
	return result
}
