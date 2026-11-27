package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/mimi6060/GeoNote/backend/internal/middleware"
	"github.com/mimi6060/GeoNote/backend/internal/model"
	"github.com/mimi6060/GeoNote/backend/internal/repository"
	"github.com/mimi6060/GeoNote/backend/internal/service"
)

type memUserStore struct {
	hashes    map[string]string
	deleteErr error
}

func (m *memUserStore) Create(context.Context, *model.User) error { return nil }
func (m *memUserStore) GetByEmail(context.Context, string) (*model.User, error) {
	return nil, repository.ErrNotFound
}
func (m *memUserStore) GetByID(context.Context, string) (*model.User, error) {
	return nil, repository.ErrNotFound
}
func (m *memUserStore) GetPasswordHash(_ context.Context, id string) (string, error) {
	h, ok := m.hashes[id]
	if !ok {
		return "", repository.ErrNotFound
	}
	return h, nil
}
func (m *memUserStore) Delete(_ context.Context, id string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	if _, ok := m.hashes[id]; !ok {
		return repository.ErrNotFound
	}
	delete(m.hashes, id)
	return nil
}

func TestDeleteAccountHandler(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		userID     string
		body       string
		wantStatus int
		wantCode   string
		wantGone   bool
		deleteErr  error
	}{
		{"deleted", "u1", `{"password":"password123"}`, http.StatusOK, "", true, nil},
		{"wrong password", "u1", `{"password":"nope"}`, http.StatusUnauthorized, "WRONG_PASSWORD", false, nil},
		{"missing password", "u1", `{}`, http.StatusBadRequest, "VALIDATION_ERROR", false, nil},
		{"no body", "u1", ``, http.StatusBadRequest, "INVALID_JSON", false, nil},
		{"unknown user", "ghost", `{"password":"password123"}`, http.StatusNotFound, "USER_NOT_FOUND", false, nil},
		{"database error", "u1", `{"password":"password123"}`, http.StatusInternalServerError, "DELETE_FAILED", false, errors.New("db down")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := &memUserStore{hashes: map[string]string{"u1": string(hash)}, deleteErr: tc.deleteErr}
			h := NewAuthHandler(service.NewAuthService(store, "secret", time.Hour))

			req := httptest.NewRequest(http.MethodDelete, "/api/v1/me", strings.NewReader(tc.body))
			req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, tc.userID))
			rec := httptest.NewRecorder()
			h.DeleteAccount(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			var resp model.APIResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("invalid JSON response: %v", err)
			}
			if tc.wantCode != "" && (resp.Error == nil || resp.Error.Code != tc.wantCode) {
				t.Fatalf("error = %+v, want code %s", resp.Error, tc.wantCode)
			}
			_, stillThere := store.hashes["u1"]
			if tc.wantGone == stillThere {
				t.Fatalf("u1 still present = %v, want gone = %v", stillThere, tc.wantGone)
			}
		})
	}
}
