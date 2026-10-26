package repository

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"

	_ "github.com/lib/pq"
)

// TestMigrationsOnRealPostgres runs the whole migration chain against a
// real PostgreSQL instance, then asserts the gemini image-generation gate
// migration (259) flips only gemini/antigravity groups.
//
// It only runs when S2A_VERIFY_DSN is set:
//
//	S2A_VERIFY_DSN='postgres://postgres:test@127.0.0.1:55433/sub2api?sslmode=disable' \
//	  go test ./internal/repository -run TestMigrationsOnRealPostgres -v
func TestMigrationsOnRealPostgres(t *testing.T) {
	dsn := os.Getenv("S2A_VERIFY_DSN")
	if dsn == "" {
		t.Skip("S2A_VERIFY_DSN not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	// Apply the full chain twice: the second run must be a no-op (idempotency).
	for i := 0; i < 2; i++ {
		if err := ApplyMigrations(ctx, db); err != nil {
			t.Fatalf("apply migrations (run %d): %v", i+1, err)
		}
	}

	var applied int
	if err := db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&applied); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	t.Logf("applied migrations: %d", applied)

	var gateFile string
	if err := db.QueryRow(
		`SELECT filename FROM schema_migrations WHERE filename LIKE '%enable_gemini_image_generation%'`,
	).Scan(&gateFile); err != nil {
		t.Fatalf("gate migration not recorded: %v", err)
	}
	t.Logf("gate migration recorded as: %s", gateFile)

	// Seed groups across platforms with the switch off, then re-run migration 259
	// exactly as it shipped to prove the UPDATE only touches gemini/antigravity.
	groupCols := `(name, platform, allow_image_generation, created_at, updated_at)`
	for i, tc := range []struct {
		name     string
		platform string
	}{
		{"verify-gemini", "gemini"},
		{"verify-antigravity", "antigravity"},
		{"verify-composite", "composite"},
		{"verify-openai", "openai"},
		{"verify-grok", "grok"},
	} {
		if _, err := db.Exec(
			`INSERT INTO groups `+groupCols+` VALUES ($1,$2,false,NOW(),NOW())`,
			fmt.Sprintf("%s-%d", tc.name, i), tc.platform,
		); err != nil {
			t.Fatalf("seed %s: %v", tc.name, err)
		}
	}

	body, err := os.ReadFile("../../migrations/259_enable_gemini_image_generation_groups.sql")
	if err != nil {
		t.Fatalf("read migration body: %v", err)
	}
	if _, err := db.Exec(string(body)); err != nil {
		t.Fatalf("execute migration 259 body: %v", err)
	}

	want := map[string]bool{
		"gemini":      true,
		"antigravity": true,
		"composite":   false,
		"openai":      false,
		"grok":        false,
	}
	for platform, expected := range want {
		var on bool
		if err := db.QueryRow(
			`SELECT allow_image_generation FROM groups WHERE platform = $1 AND name LIKE 'verify-%' LIMIT 1`,
			platform,
		).Scan(&on); err != nil {
			t.Fatalf("read %s: %v", platform, err)
		}
		if on != expected {
			t.Errorf("platform %s allow_image_generation = %v, want %v", platform, on, expected)
		}
	}
}
