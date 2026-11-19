package repository

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestUserRepoDelete runs against a real PostgreSQL + PostGIS database with
// every migration applied. It is skipped unless GEONOTE_POSTGIS_TEST_URL is
// set (the CI database has no PostGIS, so the migrations cannot run there).
// WARNING: the test creates and deletes rows; never point it at production.
func TestUserRepoDelete(t *testing.T) {
	url := os.Getenv("GEONOTE_POSTGIS_TEST_URL")
	if url == "" {
		t.Skip("GEONOTE_POSTGIS_TEST_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	count := func(sql string, args ...any) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return n
	}

	const (
		victim = "d0000000-0000-0000-0000-00000000dead"
		other  = "d0000000-0000-0000-0000-00000000beef"
		ownMsg = "d0000000-0000-0000-0000-0000000000a1"
		othMsg = "d0000000-0000-0000-0000-0000000000b1"
	)
	cleanup := func() {
		pool.Exec(ctx, `DELETE FROM users WHERE id IN ($1, $2)`, victim, other)              //nolint:errcheck
		pool.Exec(ctx, `DELETE FROM beta_signups WHERE email LIKE 'del\_%@example.invalid'`) //nolint:errcheck
	}
	cleanup()
	t.Cleanup(cleanup)

	exec(`INSERT INTO users (id, username, email, password_hash) VALUES
	      ($1, 'del_victim', 'del_victim@example.invalid', 'x'),
	      ($2, 'del_other',  'del_other@example.invalid',  'x')`, victim, other)
	exec(`INSERT INTO messages (id, user_id, content, location) VALUES
	      ($1, $2, 'own',   ST_SetSRID(ST_MakePoint(4.35, 50.85), 4326)),
	      ($3, $4, 'other', ST_SetSRID(ST_MakePoint(4.35, 50.85), 4326))`,
		ownMsg, victim, othMsg, other)
	// The victim likes, comments, reacts, unlocks and reports the other
	// user's message; the other user likes the victim's message.
	exec(`INSERT INTO interactions (message_id, user_id, type, content) VALUES
	      ($1, $2, 'like', NULL), ($1, $2, 'comment', 'hi'), ($3, $4, 'like', NULL)`,
		othMsg, victim, ownMsg, other)
	exec(`INSERT INTO reactions (message_id, user_id, emoji) VALUES ($1, $2, '🔥')`, othMsg, victim)
	exec(`INSERT INTO message_unlocks (message_id, user_id) VALUES ($1, $2), ($3, $2)`, othMsg, victim, ownMsg)
	exec(`INSERT INTO beta_signups (email) VALUES ('del_victim@example.invalid'), ('del_other@example.invalid')`)
	exec(`INSERT INTO content_reports (reporter_id, message_id, reason) VALUES ($1, $2, 'spam')`, victim, othMsg)
	exec(`INSERT INTO user_streaks (user_id, total_posts) VALUES ($1, 1)`, victim)
	exec(`INSERT INTO user_badges (user_id, badge_type) VALUES ($1, 'first_post')`, victim)

	repo := NewUserRepo(pool)
	if err := repo.Delete(ctx, victim); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if n := count(`SELECT COUNT(*) FROM users WHERE id = $1`, victim); n != 0 {
		t.Fatal("user still present")
	}
	for _, q := range []string{
		`SELECT COUNT(*) FROM messages WHERE user_id = $1`,
		`SELECT COUNT(*) FROM interactions WHERE user_id = $1`,
		`SELECT COUNT(*) FROM reactions WHERE user_id = $1`,
		`SELECT COUNT(*) FROM message_unlocks WHERE user_id = $1`,
		`SELECT COUNT(*) FROM content_reports WHERE reporter_id = $1`,
		`SELECT COUNT(*) FROM user_streaks WHERE user_id = $1`,
		`SELECT COUNT(*) FROM user_badges WHERE user_id = $1`,
	} {
		if n := count(q, victim); n != 0 {
			t.Errorf("%s: %d rows left", q, n)
		}
	}
	// The other user's message and its counters are kept and corrected.
	var likes, comments, unlocks int
	if err := pool.QueryRow(ctx,
		`SELECT likes_count, comments_count, unlocks_count FROM messages WHERE id = $1`, othMsg,
	).Scan(&likes, &comments, &unlocks); err != nil {
		t.Fatalf("other message: %v", err)
	}
	if likes != 0 || comments != 0 || unlocks != 0 {
		t.Errorf("counters = likes %d, comments %d, unlocks %d; want 0,0,0", likes, comments, unlocks)
	}
	if n := count(`SELECT COUNT(*) FROM beta_signups WHERE email = 'del_victim@example.invalid'`); n != 0 {
		t.Error("beta signup of the deleted user still present")
	}
	if n := count(`SELECT COUNT(*) FROM beta_signups WHERE email = 'del_other@example.invalid'`); n != 1 {
		t.Error("beta signup of the other user must be kept")
	}
	if n := count(`SELECT COUNT(*) FROM users WHERE id = $1`, other); n != 1 {
		t.Error("other user must not be deleted")
	}

	if err := repo.Delete(ctx, victim); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Delete = %v, want ErrNotFound", err)
	}
}
