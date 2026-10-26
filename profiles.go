package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/labstack/echo/v4"
)

var errFlockOwner = errors.New("Transfer ownership or delete every flock owned by this account before deleting the account.")

type FlockMembership struct {
	ID                        int64
	Name, Region, Topic, Role string
}

func (s *Store) MemberFlocks(ctx context.Context, memberID int64) ([]FlockMembership, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT f.id, f.name, f.region, f.topic, fm.role
		FROM flock_members fm JOIN flocks f ON f.id = fm.flock_id
		WHERE fm.member_id = ? ORDER BY f.name COLLATE NOCASE, f.id`, memberID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var flocks []FlockMembership
	for rows.Next() {
		var flock FlockMembership
		if err := rows.Scan(&flock.ID, &flock.Name, &flock.Region, &flock.Topic, &flock.Role); err != nil {
			return nil, err
		}
		flocks = append(flocks, flock)
	}
	return flocks, rows.Err()
}

func (s *Store) MemberPublicChirps(ctx context.Context, memberID int64) ([]ChirpPreview, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.id, c.member_id, c.content, m.display_name, m.username,
		c.created_at, c.flock_id, f.name FROM chirps c JOIN members m ON m.id = c.member_id
		LEFT JOIN flocks f ON f.id = c.flock_id WHERE c.member_id = ? AND c.visibility = 'public'
		ORDER BY c.created_at DESC, c.id DESC LIMIT 10`, memberID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var chirps []ChirpPreview
	for rows.Next() {
		var chirp ChirpPreview
		if err := rows.Scan(&chirp.ID, &chirp.MemberID, &chirp.Content, &chirp.DisplayName,
			&chirp.Username, &chirp.CreatedAt, &chirp.FlockID, &chirp.FlockName); err != nil {
			return nil, err
		}
		chirps = append(chirps, chirp)
	}
	return chirps, rows.Err()
}

func (s *Store) UpdateMember(ctx context.Context, member Member) error {
	result, err := s.db.ExecContext(ctx, `UPDATE members SET username = ?, display_name = ?, region = ?, bio = ?, updated_at = ? WHERE id = ?`,
		member.Username, member.DisplayName, member.Region, member.Bio, time.Now().UTC().Format(time.RFC3339), member.ID)
	if err != nil {
		return accountError(err)
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return sql.ErrNoRows
	}
	return err
}

// Ownership checks, creator-reference repair, and cascaded account deletion share
// one transaction. File paths are returned only after a successful commit.
func (s *Store) DeleteAccount(ctx context.Context, memberID int64) ([]string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var userID int64
	if err := tx.QueryRowContext(ctx, "SELECT user_id FROM members WHERE id = ?", memberID).Scan(&userID); err != nil {
		return nil, err
	}
	var owned int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM flock_members WHERE member_id = ? AND role = 'owner'`, memberID).Scan(&owned); err != nil {
		return nil, err
	}
	if owned != 0 {
		return nil, errFlockOwner
	}
	// A creator may delete their account after transferring ownership. Abort if
	// legacy/incomplete flock data has no remaining owner to take the reference.
	var unowned int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM flocks f WHERE f.created_by = ?
		AND NOT EXISTS (SELECT 1 FROM flock_members fm WHERE fm.flock_id = f.id AND fm.role = 'owner' AND fm.member_id <> ?)`, memberID, memberID).Scan(&unowned); err != nil {
		return nil, err
	}
	if unowned != 0 {
		return nil, errFlockOwner
	}
	if _, err := tx.ExecContext(ctx, `UPDATE flocks SET created_by =
		(SELECT member_id FROM flock_members WHERE flock_id = flocks.id AND role = 'owner') WHERE created_by = ?`, memberID); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT image_path FROM chirps WHERE member_id = ? AND image_path IS NOT NULL", memberID)
	if err != nil {
		return nil, err
	}
	var photos []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			rows.Close()
			return nil, err
		}
		photos = append(photos, path)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	// Foreign keys cascade to sessions, profile, chirps, tags, and memberships.
	if _, err := tx.ExecContext(ctx, "DELETE FROM users WHERE id = ?", userID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return photos, nil
}

func (a *application) profileMember(c echo.Context, requireOwner bool) (*Member, error) {
	if requireOwner && currentMember(c) == nil {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "Sign in to manage this profile.")
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		return nil, echo.NewHTTPError(http.StatusNotFound, "This member could not be found.")
	}
	member, err := a.store.Member(c.Request().Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, echo.NewHTTPError(http.StatusNotFound, "This member could not be found.")
	}
	if err != nil {
		return nil, err
	}
	if requireOwner && currentMember(c).ID != member.ID {
		return nil, echo.NewHTTPError(http.StatusForbidden, "Only the account owner can manage this profile.")
	}
	return member, nil
}

func (a *application) renderProfile(c echo.Context, status int, member *Member, editing bool, message string) error {
	chirps, err := a.store.MemberPublicChirps(c.Request().Context(), member.ID)
	if err != nil {
		return err
	}
	flocks, err := a.store.MemberFlocks(c.Request().Context(), member.ID)
	if err != nil {
		return err
	}
	owner := currentMember(c) != nil && currentMember(c).ID == member.ID
	return a.render(c, status, "profile.html", pageData{Title: member.DisplayName, Profile: member,
		Chirps: chirps, Memberships: flocks, Owner: owner, Editing: owner && editing, Error: message})
}

func (a *application) profile(c echo.Context) error {
	member, err := a.profileMember(c, false)
	if err != nil {
		return err
	}
	return a.renderProfile(c, http.StatusOK, member, c.QueryParam("edit") == "1", "")
}

func (a *application) updateProfile(c echo.Context) error {
	member, err := a.profileMember(c, true)
	if err != nil {
		return err
	}
	member.Username = strings.ToLower(strings.TrimSpace(c.FormValue("username")))
	member.DisplayName = strings.TrimSpace(c.FormValue("display_name"))
	member.Region = strings.TrimSpace(c.FormValue("region"))
	member.Bio = strings.TrimSpace(c.FormValue("bio"))
	message := ""
	switch {
	case !usernamePattern.MatchString(member.Username):
		message = "Use 3–24 letters, numbers, or underscores for your username."
	case !utf8.ValidString(member.DisplayName) || utf8.RuneCountInString(member.DisplayName) < 1 || utf8.RuneCountInString(member.DisplayName) > 80:
		message = "Enter a display name between 1 and 80 characters."
	case !utf8.ValidString(member.Region) || utf8.RuneCountInString(member.Region) > 100:
		message = "Keep the region to 100 characters or fewer."
	case !utf8.ValidString(member.Bio) || utf8.RuneCountInString(member.Bio) > 500:
		message = "Keep the bio to 500 characters or fewer."
	}
	if message != "" {
		return a.renderProfile(c, http.StatusUnprocessableEntity, member, true, message)
	}
	if err := a.store.UpdateMember(c.Request().Context(), *member); err != nil {
		if errors.Is(err, errDuplicateAccount) {
			return a.renderProfile(c, http.StatusConflict, member, true, "That username is already registered.")
		}
		return err
	}
	return c.Redirect(http.StatusSeeOther, fmt.Sprintf("/members/%d", member.ID))
}

func (a *application) deleteAccount(c echo.Context) error {
	member, err := a.profileMember(c, true)
	if err != nil {
		return err
	}
	if c.FormValue("confirm_delete") != "yes" {
		return a.renderProfile(c, http.StatusUnprocessableEntity, member, false, "Confirm permanent account deletion before continuing.")
	}
	photos, err := a.store.DeleteAccount(c.Request().Context(), member.ID)
	if errors.Is(err, errFlockOwner) {
		return a.renderProfile(c, http.StatusConflict, member, false, errFlockOwner.Error())
	}
	if err != nil {
		return err
	}
	uploadDir := os.Getenv("UPLOAD_DIR")
	if uploadDir == "" {
		uploadDir = "./data/uploads"
	}
	for _, photo := range photos {
		// Uploads use random flat filenames. Never remove paths outside the upload
		// directory, including legacy or manually inserted database values.
		if photo == "" || photo == "." || photo == ".." || filepath.Base(photo) != photo || strings.ContainsAny(photo, "/\\") {
			c.Logger().Error("Skipped invalid stored photo filename during account cleanup")
			continue
		}
		if err := os.Remove(filepath.Join(uploadDir, photo)); err != nil && !errors.Is(err, os.ErrNotExist) {
			c.Logger().Errorf("Account photo cleanup failed: %v", err)
		}
	}
	a.setCookie(c, sessionCookie, "", -time.Hour)
	a.setCookie(c, csrfCookie, "", -time.Hour)
	return c.Redirect(http.StatusSeeOther, "/")
}
