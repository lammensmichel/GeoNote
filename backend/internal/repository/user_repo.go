package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mimi6060/GeoNote/backend/internal/model"
)

type UserRepo struct {
	pool *pgxpool.Pool
}

func NewUserRepo(pool *pgxpool.Pool) *UserRepo {
	return &UserRepo{pool: pool}
}

func (r *UserRepo) Create(ctx context.Context, user *model.User) error {
	return r.pool.QueryRow(ctx,
		`INSERT INTO users (username, email, password_hash)
		 VALUES ($1, $2, $3)
		 RETURNING id, created_at`,
		user.Username, user.Email, user.PasswordHash,
	).Scan(&user.ID, &user.CreatedAt)
}

func (r *UserRepo) GetByEmail(ctx context.Context, email string) (*model.User, error) {
	u := &model.User{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, username, email, password_hash, avatar_url, is_anonymous, created_at
		 FROM users WHERE email = $1`,
		email,
	).Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.AvatarURL, &u.IsAnonymous, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (r *UserRepo) GetByID(ctx context.Context, id string) (*model.User, error) {
	u := &model.User{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, username, email, avatar_url, is_anonymous, created_at
		 FROM users WHERE id = $1`,
		id,
	).Scan(&u.ID, &u.Username, &u.Email, &u.AvatarURL, &u.IsAnonymous, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

// GetPasswordHash returns the bcrypt hash of a user's password.
// Returns ErrNotFound if the user does not exist.
func (r *UserRepo) GetPasswordHash(ctx context.Context, id string) (string, error) {
	var hash string
	err := r.pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1`, id).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return hash, err
}

// Delete removes a user. Every row linked to the user (messages,
// interactions, reactions, unlocks, reports, streaks, badges) is removed
// by ON DELETE CASCADE. Like and comment counters on other users' messages
// are kept in sync by the interactions trigger; the unlock counter has no
// DELETE trigger, so it is decremented here in the same transaction.
// Returns ErrNotFound if nothing was deleted.
func (r *UserRepo) Delete(ctx context.Context, id string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	if _, err := tx.Exec(ctx,
		`UPDATE messages m
		 SET unlocks_count = GREATEST(m.unlocks_count - 1, 0)
		 FROM message_unlocks u
		 WHERE u.message_id = m.id AND u.user_id = $1 AND m.user_id <> $1`,
		id,
	); err != nil {
		return err
	}

	tag, err := tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return tx.Commit(ctx)
}

// SearchByUsername searches users by username with ILIKE pattern matching.
func (r *UserRepo) SearchByUsername(ctx context.Context, query string, limit, offset int) ([]model.UserSummary, int, error) {
	pattern := "%" + query + "%"

	// Get total count
	var total int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM users WHERE username ILIKE $1`,
		pattern,
	).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// Get paginated results
	rows, err := r.pool.Query(ctx,
		`SELECT id, username FROM users
		 WHERE username ILIKE $1
		 ORDER BY username ASC
		 LIMIT $2 OFFSET $3`,
		pattern, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var users []model.UserSummary
	for rows.Next() {
		var u model.UserSummary
		if err := rows.Scan(&u.ID, &u.Username); err != nil {
			return nil, 0, err
		}
		users = append(users, u)
	}
	return users, total, nil
}
