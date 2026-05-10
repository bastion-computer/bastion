package secret_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/database"
	"github.com/bastion-computer/bastion/core/internal/failure"
	"github.com/bastion-computer/bastion/core/internal/secret"
)

func TestServiceCreatesListsResolvesAndRemovesSecret(t *testing.T) {
	db := openDB(t)
	service := secret.New(db)
	ctx := context.Background()

	t.Setenv("BASTION_SECRET_TEST", "secret-value")

	created := createSecret(ctx, t, service)
	assertSecretList(ctx, t, service)
	assertSecretGet(ctx, t, service, created.ID)
	assertSecretResolve(ctx, t, service, created.ID)
	assertSecretRemove(ctx, t, service, created.ID)
}

func createSecret(ctx context.Context, t *testing.T, service *secret.Service) secret.Secret {
	t.Helper()

	created, err := service.Create(ctx, secret.CreateRequest{
		Key:        "API_KEY",
		Env:        "BASTION_SECRET_TEST",
		AllowHosts: []string{"*.example.com", "", "*.example.com", "api.example.com"},
	})
	if err != nil {
		t.Fatalf("create secret: %v", err)
	}

	if created.ID == "" || created.Key != "API_KEY" || len(created.AllowHosts) != 2 {
		t.Fatalf("unexpected created secret: %#v", created)
	}

	return created
}

func assertSecretList(ctx context.Context, t *testing.T, service *secret.Service) {
	t.Helper()

	page, err := service.List(ctx, 20, "")
	if err != nil {
		t.Fatalf("list secrets: %v", err)
	}

	if len(page.Entries) != 1 || page.Cursor != nil {
		t.Fatalf("unexpected secrets page: %#v", page)
	}
}

func assertSecretGet(ctx context.Context, t *testing.T, service *secret.Service, wantID string) {
	t.Helper()

	got, err := service.Get(ctx, "", "API_KEY")
	if err != nil {
		t.Fatalf("get secret: %v", err)
	}

	if got.ID != wantID || len(got.AllowHosts) != 2 {
		t.Fatalf("unexpected secret: %#v", got)
	}
}

func assertSecretResolve(ctx context.Context, t *testing.T, service *secret.Service, id string) {
	t.Helper()

	resolved, err := service.Resolve(ctx, id, "")
	if err != nil {
		t.Fatalf("resolve secret: %v", err)
	}

	if resolved.Value != "secret-value" {
		t.Fatalf("resolved value = %q, want secret-value", resolved.Value)
	}
}

func assertSecretRemove(ctx context.Context, t *testing.T, service *secret.Service, id string) {
	t.Helper()

	removed, err := service.Remove(ctx, id, "")
	if err != nil {
		t.Fatalf("remove secret: %v", err)
	}

	if removed.ID != id {
		t.Fatalf("removed secret id = %q, want %q", removed.ID, id)
	}

	if _, err := service.Get(ctx, id, ""); !errors.Is(err, failure.ErrNotFound) {
		t.Fatalf("get removed secret error = %v, want not found", err)
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
