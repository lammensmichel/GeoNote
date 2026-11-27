package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/mimi6060/GeoNote/backend/internal/model"
	"github.com/mimi6060/GeoNote/backend/internal/repository"
)

// fakeUserStore is an in-memory UserStore for tests.
type fakeUserStore struct {
	hashes    map[string]string
	deleted   []string
	deleteErr error
}

func (f *fakeUserStore) Create(context.Context, *model.User) error { return nil }
func (f *fakeUserStore) GetByEmail(context.Context, string) (*model.User, error) {
	return nil, repository.ErrNotFound
}
func (f *fakeUserStore) GetByID(context.Context, string) (*model.User, error) {
	return nil, repository.ErrNotFound
}
func (f *fakeUserStore) GetPasswordHash(_ context.Context, id string) (string, error) {
	h, ok := f.hashes[id]
	if !ok {
		return "", repository.ErrNotFound
	}
	return h, nil
}
func (f *fakeUserStore) Delete(_ context.Context, id string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	if _, ok := f.hashes[id]; !ok {
		return repository.ErrNotFound
	}
	delete(f.hashes, id)
	f.deleted = append(f.deleted, id)
	return nil
}

func newStoreWithUser(t *testing.T, id, password string) *fakeUserStore {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return &fakeUserStore{hashes: map[string]string{id: string(hash)}}
}

func TestDeleteAccount(t *testing.T) {
	ctx := context.Background()

	t.Run("right password deletes and runs hook", func(t *testing.T) {
		store := newStoreWithUser(t, "u1", "password123")
		svc := NewAuthService(store, "secret", time.Hour)
		hookCalls := 0
		svc.OnAccountDeleted(func(context.Context) { hookCalls++ })

		if err := svc.DeleteAccount(ctx, "u1", "password123"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(store.deleted) != 1 || store.deleted[0] != "u1" {
			t.Fatalf("user not deleted: %v", store.deleted)
		}
		if hookCalls != 1 {
			t.Fatalf("hook called %d times, want 1", hookCalls)
		}
	})

	t.Run("wrong password keeps the account", func(t *testing.T) {
		store := newStoreWithUser(t, "u1", "password123")
		svc := NewAuthService(store, "secret", time.Hour)
		hookCalls := 0
		svc.OnAccountDeleted(func(context.Context) { hookCalls++ })

		err := svc.DeleteAccount(ctx, "u1", "wrong-password")
		if !errors.Is(err, ErrWrongPassword) {
			t.Fatalf("got %v, want ErrWrongPassword", err)
		}
		if len(store.deleted) != 0 || hookCalls != 0 {
			t.Fatal("account must not be deleted with a wrong password")
		}
	})

	t.Run("unknown user", func(t *testing.T) {
		svc := NewAuthService(&fakeUserStore{hashes: map[string]string{}}, "secret", time.Hour)
		if err := svc.DeleteAccount(ctx, "ghost", "password123"); !errors.Is(err, ErrUserNotFound) {
			t.Fatalf("got %v, want ErrUserNotFound", err)
		}
	})

	t.Run("database error is returned and hook not run", func(t *testing.T) {
		store := newStoreWithUser(t, "u1", "password123")
		store.deleteErr = errors.New("db down")
		svc := NewAuthService(store, "secret", time.Hour)
		hookCalls := 0
		svc.OnAccountDeleted(func(context.Context) { hookCalls++ })

		err := svc.DeleteAccount(ctx, "u1", "password123")
		if err == nil || errors.Is(err, ErrWrongPassword) || errors.Is(err, ErrUserNotFound) {
			t.Fatalf("got %v, want the raw database error", err)
		}
		if hookCalls != 0 {
			t.Fatal("hook must not run when deletion failed")
		}
	})
}
