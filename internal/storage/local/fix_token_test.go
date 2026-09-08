package local

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DoplexLabs/belay-engine/internal/canonical/model"
)

func TestFixActionTokenRoundTripTamperingAndStoreBinding(t *testing.T) {
	store := openStorageTestStore(t)
	_, issueID, err := store.DeriveIssueIdentity(
		"failure.v1",
		"explicit_command_failure",
		"scope-token",
		"dimension",
	)
	if err != nil {
		t.Fatal(err)
	}
	issuedAt := time.Date(2026, 9, 8, 17, 0, 0, 0, time.UTC)
	claims := model.FixActionClaims{
		Version:   model.FixActionTokenVersion,
		IssueID:   issueID,
		Snapshot:  17,
		IssuedAt:  issuedAt,
		ExpiresAt: issuedAt.Add(issueCursorLifetime),
	}
	token, err := store.IssueFixActionToken(claims)
	if err != nil {
		t.Fatalf("IssueFixActionToken() error = %v", err)
	}
	decoded, err := store.DecodeFixActionToken(token)
	if err != nil {
		t.Fatalf("DecodeFixActionToken() error = %v", err)
	}
	if decoded != claims {
		t.Fatalf("decoded claims = %+v, want %+v", decoded, claims)
	}

	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		t.Fatalf("token parts = %d, want 2", len(parts))
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(body) == 0 {
		t.Fatalf("decode token body = %d bytes, %v", len(body), err)
	}
	body[0] ^= 0x01
	tamperedBody := base64.RawURLEncoding.EncodeToString(body)
	if tamperedBody == parts[0] {
		t.Fatal("tampered token body did not change")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(signature) == 0 {
		t.Fatalf("decode token signature = %d bytes, %v", len(signature), err)
	}
	signature[0] ^= 0x01
	tamperedSignature := base64.RawURLEncoding.EncodeToString(signature)
	if tamperedSignature == parts[1] {
		t.Fatal("tampered token signature did not change")
	}
	for _, invalid := range []string{
		tamperedBody + "." + parts[1],
		parts[0] + "." + tamperedSignature,
		token + ".extra",
		"",
	} {
		if _, err := store.DecodeFixActionToken(invalid); !errors.Is(err, ErrFixActionTokenInvalid) {
			t.Errorf("DecodeFixActionToken(tampered) error = %v", err)
		}
	}
	if _, err := openStorageTestStore(t).DecodeFixActionToken(token); !errors.Is(
		err,
		ErrFixActionTokenInvalid,
	) {
		t.Fatalf("token from another store error = %v", err)
	}
}

func TestFixActionTokenValidatesStructureButNotCurrentExpiry(t *testing.T) {
	store := openStorageTestStore(t)
	_, issueID, err := store.DeriveIssueIdentity("v1", "detector", "scope", "dimension")
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	expiredClaims := model.FixActionClaims{
		Version:   model.FixActionTokenVersion,
		IssueID:   issueID,
		Snapshot:  1,
		IssuedAt:  base,
		ExpiresAt: base.Add(time.Minute),
	}
	token, err := store.IssueFixActionToken(expiredClaims)
	if err != nil {
		t.Fatal(err)
	}
	if decoded, err := store.DecodeFixActionToken(token); err != nil || decoded != expiredClaims {
		t.Fatalf("expired structural decode = (%+v, %v)", decoded, err)
	}

	for _, claims := range []model.FixActionClaims{
		{},
		{
			Version:   "wrong",
			IssueID:   issueID,
			Snapshot:  1,
			IssuedAt:  base,
			ExpiresAt: base.Add(time.Minute),
		},
		{
			Version:   model.FixActionTokenVersion,
			IssueID:   "iss_invalid",
			Snapshot:  1,
			IssuedAt:  base,
			ExpiresAt: base.Add(time.Minute),
		},
		{
			Version:   model.FixActionTokenVersion,
			IssueID:   issueID,
			Snapshot:  1,
			IssuedAt:  base,
			ExpiresAt: base.Add(issueCursorLifetime + time.Nanosecond),
		},
	} {
		if _, err := store.IssueFixActionToken(claims); !errors.Is(
			err,
			ErrFixActionTokenInvalid,
		) {
			t.Errorf("IssueFixActionToken(%+v) error = %v", claims, err)
		}
	}
}
