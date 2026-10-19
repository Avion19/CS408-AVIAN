package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const sessionLifetime = 7 * 24 * time.Hour

var errDuplicateAccount = errors.New("That email or username is already registered.")

// Member contains public profile data only, never credentials or email.
type Member struct {
	ID                                 int64
	Username, DisplayName, Region, Bio string
}

type loginSession struct {
	Member Member
	CSRF   string
}

func randomToken() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Creating credentials and their public profile is one atomic operation.
func (s *Store) Register(ctx context.Context, email, username, password string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	username = strings.ToLower(strings.TrimSpace(username))
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := tx.ExecContext(ctx, "INSERT INTO users(email, password_hash, created_at) VALUES (?, ?, ?)", email, string(hash), now)
	if err != nil {
		return accountError(err)
	}
	userID, err := result.LastInsertId()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO members(user_id, username, display_name, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)`, userID, username, username, now, now)
	if err != nil {
		return accountError(err)
	}
	return tx.Commit()
}

func accountError(err error) error {
	if strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return errDuplicateAccount
	}
	return err
}

// Run a bcrypt comparison even for an unknown email to avoid a fast failure path.
var unknownPasswordHash, _ = bcrypt.GenerateFromPassword([]byte("unused-account-password"), bcrypt.DefaultCost)

func (s *Store) Authenticate(ctx context.Context, email, password string) (int64, error) {
	var id int64
	var hash string
	err := s.db.QueryRowContext(ctx, "SELECT id, password_hash FROM users WHERE lower(email) = ?", strings.ToLower(strings.TrimSpace(email))).Scan(&id, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		bcrypt.CompareHashAndPassword(unknownPasswordHash, []byte(password))
		return 0, sql.ErrNoRows
	}
	if err != nil {
		return 0, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return 0, sql.ErrNoRows
	}
	return id, nil
}

func (s *Store) CreateSession(ctx context.Context, userID int64) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	csrf, err := randomToken()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at <= ?", now.Format(time.RFC3339)); err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO sessions(user_id, token_hash, csrf_token, expires_at, created_at) VALUES (?, ?, ?, ?, ?)`,
		userID, tokenHash(token), csrf, now.Add(sessionLifetime).Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return token, nil
}

func (s *Store) Session(ctx context.Context, token string) (*loginSession, error) {
	var session loginSession
	err := s.db.QueryRowContext(ctx, `SELECT m.id, m.username, m.display_name, m.region, m.bio, s.csrf_token
		FROM sessions s JOIN members m ON m.user_id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ?`, tokenHash(token), time.Now().UTC().Format(time.RFC3339)).Scan(
		&session.Member.ID, &session.Member.Username, &session.Member.DisplayName, &session.Member.Region, &session.Member.Bio, &session.CSRF)
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ?", tokenHash(token))
	return err
}

func (s *Store) Member(ctx context.Context, id int64) (*Member, error) {
	var member Member
	err := s.db.QueryRowContext(ctx, "SELECT id, username, display_name, region, bio FROM members WHERE id = ?", id).Scan(
		&member.ID, &member.Username, &member.DisplayName, &member.Region, &member.Bio)
	if err != nil {
		return nil, err
	}
	return &member, nil
}

type ChirpPreview struct {
	ID, MemberID                              int64
	Content, DisplayName, Username, CreatedAt string
	FlockID                                   sql.NullInt64
	FlockName                                 sql.NullString
}

func (s *Store) RecentPublicChirps(ctx context.Context) ([]ChirpPreview, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.id, c.member_id, c.content, m.display_name, m.username, c.created_at, c.flock_id, f.name
		FROM chirps c JOIN members m ON m.id = c.member_id LEFT JOIN flocks f ON f.id = c.flock_id
		WHERE c.visibility = 'public' ORDER BY c.created_at DESC, c.id DESC LIMIT 3`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []ChirpPreview
	for rows.Next() {
		var chirp ChirpPreview
		if err := rows.Scan(&chirp.ID, &chirp.MemberID, &chirp.Content, &chirp.DisplayName, &chirp.Username, &chirp.CreatedAt, &chirp.FlockID, &chirp.FlockName); err != nil {
			return nil, err
		}
		result = append(result, chirp)
	}
	return result, rows.Err()
}
