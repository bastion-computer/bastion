package secret_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/database"
	"github.com/bastion-computer/bastion/core/internal/failure"
	"github.com/bastion-computer/bastion/core/internal/services/secret"
)

func TestServiceCreatesListsGetsAndRemovesSecret(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	service := secret.NewService(db)
	ctx := context.Background()
	key := "api-token"

	created, err := service.Create(ctx, secret.CreateRequest{Key: &key, Value: "sensitive-value"})
	if err != nil {
		t.Fatalf("create secret: %v", err)
	}

	assertCreatedSecretMetadata(t, created, key, "sensitive-value")
	assertSecretList(t, service, created.ID)
	assertSecretGet(t, service, key, created.ID, "sensitive-value")
	assertSecretRemove(t, service, created.ID, "sensitive-value")
}

func assertCreatedSecretMetadata(t *testing.T, created secret.Metadata, key, value string) {
	t.Helper()

	if !strings.HasPrefix(created.ID, "sec_") {
		t.Fatalf("secret id = %q, want sec_ prefix", created.ID)
	}

	requireSecretKey(t, created.Key, key)

	encoded, err := json.Marshal(created)
	if err != nil {
		t.Fatalf("marshal created secret metadata: %v", err)
	}

	if strings.Contains(string(encoded), value) || strings.Contains(string(encoded), `"value"`) {
		t.Fatalf("created secret metadata leaked value: %s", encoded)
	}
}

func assertSecretList(t *testing.T, service *secret.Service, secretID string) {
	t.Helper()

	page, err := service.List(context.Background(), 20, "")
	if err != nil {
		t.Fatalf("list secrets: %v", err)
	}

	if len(page.Entries) != 1 || page.Cursor != nil || page.Entries[0].ID != secretID {
		t.Fatalf("unexpected secrets page: %#v", page)
	}
}

func assertSecretGet(t *testing.T, service *secret.Service, key, secretID, value string) {
	t.Helper()

	got, err := service.Get(context.Background(), "", key)
	if err != nil {
		t.Fatalf("get secret: %v", err)
	}

	if got.ID != secretID || got.Value != value {
		t.Fatalf("secret = %#v, want created secret with value", got)
	}

	requireSecretKey(t, got.Key, key)
}

func assertSecretRemove(t *testing.T, service *secret.Service, secretID, value string) {
	t.Helper()

	ctx := context.Background()

	removed, err := service.Remove(ctx, secretID, "")
	if err != nil {
		t.Fatalf("remove secret: %v", err)
	}

	if removed.ID != secretID {
		t.Fatalf("removed secret id = %q, want %q", removed.ID, secretID)
	}

	encoded, err := json.Marshal(removed)
	if err != nil {
		t.Fatalf("marshal removed secret metadata: %v", err)
	}

	if strings.Contains(string(encoded), value) || strings.Contains(string(encoded), `"value"`) {
		t.Fatalf("removed secret metadata leaked value: %s", encoded)
	}

	if _, err := service.Get(ctx, secretID, ""); !errors.Is(err, failure.ErrNotFound) {
		t.Fatalf("get removed secret error = %v, want not found", err)
	}
}

func TestServiceCreatesSecretsWithOptionalKeys(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	service := secret.NewService(db)
	ctx := context.Background()
	key := "keyed-secret"

	keyed, err := service.Create(ctx, secret.CreateRequest{Key: &key, Value: "keyed"})
	if err != nil {
		t.Fatalf("create keyed secret: %v", err)
	}

	requireSecretKey(t, keyed.Key, key)

	unkeyedOne, err := service.Create(ctx, secret.CreateRequest{Value: "first"})
	if err != nil {
		t.Fatalf("create first unkeyed secret: %v", err)
	}

	unkeyedTwo, err := service.Create(ctx, secret.CreateRequest{Value: "second"})
	if err != nil {
		t.Fatalf("create second unkeyed secret: %v", err)
	}

	if unkeyedOne.Key != nil || unkeyedTwo.Key != nil {
		t.Fatalf("unkeyed secret keys = %#v/%#v, want nil", unkeyedOne.Key, unkeyedTwo.Key)
	}

	got, err := service.Get(ctx, unkeyedTwo.ID, "")
	if err != nil {
		t.Fatalf("get unkeyed secret: %v", err)
	}

	if got.Value != "second" || got.Key != nil {
		t.Fatalf("unkeyed secret = %#v, want second value without key", got)
	}
}

func TestServiceRejectsDuplicateBlankAndEmptySecrets(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	service := secret.NewService(db)
	ctx := context.Background()
	key := "duplicate-secret"
	blankKey := ""

	if _, err := service.Create(ctx, secret.CreateRequest{Key: &key, Value: "first"}); err != nil {
		t.Fatalf("create keyed secret: %v", err)
	}

	if _, err := service.Create(ctx, secret.CreateRequest{Key: &key, Value: "second"}); !errors.Is(err, failure.ErrConflict) {
		t.Fatalf("create duplicate keyed secret error = %v, want conflict", err)
	}

	if _, err := service.Create(ctx, secret.CreateRequest{Key: &blankKey, Value: "value"}); !errors.Is(err, failure.ErrInvalid) {
		t.Fatalf("create blank-key secret error = %v, want invalid", err)
	}

	if _, err := service.Create(ctx, secret.CreateRequest{Value: ""}); !errors.Is(err, failure.ErrInvalid) {
		t.Fatalf("create empty-value secret error = %v, want invalid", err)
	}
}

func openDB(t *testing.T) *database.Client {
	t.Helper()

	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	return db
}

func requireSecretKey(t *testing.T, got *string, want string) {
	t.Helper()

	if got == nil || *got != want {
		t.Fatalf("secret key = %#v, want %q", got, want)
	}
}
