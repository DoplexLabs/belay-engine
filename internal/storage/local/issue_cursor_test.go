package local

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"

	"github.com/DoplexLabs/belay-engine/internal/canonical/model"
)

func TestIssueCursorCodecPersistsAcrossRestartAndRejectsTamperingAndOtherStore(
	t *testing.T,
) {
	path := filepath.Join(t.TempDir(), "cursor.sqlite")
	provider := newMemoryKeyProvider()
	store, err := Open(path, provider)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"kind":"issue-list","version":2}`)
	sealed, err := store.SealIssueCursor(payload)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := store.OpenIssueCursor(sealed)
	if err != nil || !bytes.Equal(opened, payload) {
		t.Fatalf("OpenIssueCursor() = %q, %v", opened, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path, provider)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if opened, err := reopened.OpenIssueCursor(sealed); err != nil ||
		!bytes.Equal(opened, payload) {
		t.Fatalf("reopened cursor = %q, %v", opened, err)
	}
	tampered := sealed[:len(sealed)-1] + "A"
	if _, err := reopened.OpenIssueCursor(tampered); !errors.Is(
		err,
		model.ErrIssueCursorInvalid,
	) {
		t.Fatalf("tampered cursor error = %v", err)
	}
	other := openStorageTestStore(t)
	if _, err := other.OpenIssueCursor(sealed); !errors.Is(
		err,
		model.ErrIssueCursorInvalid,
	) {
		t.Fatalf("cross-store cursor error = %v", err)
	}
}

func TestIssueCursorCodecRejectsEmptyAndOversizedPayloads(t *testing.T) {
	store := openStorageTestStore(t)
	if _, err := store.SealIssueCursor(nil); !errors.Is(
		err,
		model.ErrIssueCursorInvalid,
	) {
		t.Fatalf("empty payload error = %v", err)
	}
	if _, err := store.SealIssueCursor(make([]byte, maxIssueCursorBytes+1)); !errors.Is(
		err,
		model.ErrIssueCursorInvalid,
	) {
		t.Fatalf("oversized payload error = %v", err)
	}
}
