package main

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Migrations are embedded so local and EC2 startup use the same schema files.
//
//go:embed migrations/*.sql
var migrationFiles embed.FS

// Store owns the application's SQLite connection pool.
type Store struct{ db *sql.DB }

// Species is the catalog data displayed on the Checkpoint 1 Home page.
type Species struct {
	ID             int64
	CommonName     string
	ScientificName string
	Description    string
	Habitat        string
	SourceName     string
	SourceURL      string
}

// OpenStore opens SQLite and enables connection-level safety settings.
func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open SQLite: %w", err)
	}
	// PRAGMAs apply per connection. One connection also keeps migration order stable.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}
	return &Store{db: db}, nil
}

// Close releases the database connection.
func (s *Store) Close() error { return s.db.Close() }

// Ping reports whether SQLite is available to serve requests.
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// Initialize applies each pending migration once, then inserts missing species.
// Each migration and its ledger entry commit together, so a failed migration
// remains pending for the next startup.
func (s *Store) Initialize() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		var exists int
		if err := s.db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", name).Scan(&exists); err != nil {
			return err
		}
		if exists != 0 {
			continue
		}
		contents, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err = tx.Exec(string(contents)); err == nil {
			_, err = tx.Exec("INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)", name, time.Now().UTC().Format(time.RFC3339))
		}
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
	}
	return s.seedSpecies()
}

// Species returns the current catalog in a stable display order.
func (s *Store) Species(ctx context.Context) ([]Species, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, common_name, scientific_name, description, habitat, source_name, source_url
		FROM species ORDER BY common_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Species
	for rows.Next() {
		var bird Species
		if err := rows.Scan(&bird.ID, &bird.CommonName, &bird.ScientificName, &bird.Description, &bird.Habitat, &bird.SourceName, &bird.SourceURL); err != nil {
			return nil, err
		}
		result = append(result, bird)
	}
	return result, rows.Err()
}
