package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/labstack/echo/v4"
)

const chirpsPerPage = 20

var errChirpFields = errors.New("Choose existing species and a flock you belong to. Flock-only chirps require a flock.")
var errChirpOwner = errors.New("Only the author can manage this chirp.")

type Chirp struct {
	ID, MemberID                                                     int64
	Content, DisplayName, Username, CreatedAt, Visibility, ImagePath string
	FlockID                                                          sql.NullInt64
	FlockName                                                        sql.NullString
	Species                                                          []Species
	CanEdit                                                          bool
}

type ChirpForm struct {
	ID                           int64
	Content, Visibility, FlockID string
	SelectedSpecies              map[int64]bool
	ImagePath                    string
	RemovePhoto, PhotoPermission bool
}

// This predicate also protects species results and photo reads. Authorship alone
// does not grant access to a flock-only observation after leaving its flock.
const visibleChirp = `(c.visibility = 'public' OR EXISTS
 (SELECT 1 FROM flock_members fm WHERE fm.flock_id = c.flock_id AND fm.member_id = ?))`
const chirpColumns = `SELECT c.id, c.member_id, c.content, m.display_name, m.username,
 c.created_at, c.flock_id, f.name, c.visibility, COALESCE(c.image_path, '')
 FROM chirps c JOIN members m ON m.id = c.member_id LEFT JOIN flocks f ON f.id = c.flock_id`

func scanChirp(row interface{ Scan(...any) error }) (Chirp, error) {
	var chirp Chirp
	err := row.Scan(&chirp.ID, &chirp.MemberID, &chirp.Content, &chirp.DisplayName, &chirp.Username,
		&chirp.CreatedAt, &chirp.FlockID, &chirp.FlockName, &chirp.Visibility, &chirp.ImagePath)
	return chirp, err
}

func (s *Store) Chirp(ctx context.Context, id int64) (Chirp, error) {
	return scanChirp(s.db.QueryRowContext(ctx, chirpColumns+` WHERE c.id = ?`, id))
}

func (s *Store) VisibleChirp(ctx context.Context, id, viewer int64) (Chirp, error) {
	return scanChirp(s.db.QueryRowContext(ctx, chirpColumns+` WHERE c.id = ? AND `+visibleChirp, id, viewer))
}

func (s *Store) ChirpSpecies(ctx context.Context, id int64) ([]Species, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT s.id, s.common_name FROM species s
  JOIN chirp_species cs ON cs.species_id = s.id WHERE cs.chirp_id = ? ORDER BY s.common_name`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var birds []Species
	for rows.Next() {
		var bird Species
		if err := rows.Scan(&bird.ID, &bird.CommonName); err != nil {
			return nil, err
		}
		birds = append(birds, bird)
	}
	return birds, rows.Err()
}

func (s *Store) VisibleChirps(ctx context.Context, viewer, speciesID int64, page int) ([]Chirp, bool, error) {
	query := chirpColumns + ` WHERE ` + visibleChirp
	args := []any{viewer}
	if speciesID != 0 {
		query += ` AND EXISTS (SELECT 1 FROM chirp_species cs WHERE cs.chirp_id = c.id AND cs.species_id = ?)`
		args = append(args, speciesID)
	}
	query += ` ORDER BY c.created_at DESC, c.id DESC LIMIT ? OFFSET ?`
	args = append(args, chirpsPerPage+1, (page-1)*chirpsPerPage)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	var chirps []Chirp
	for rows.Next() {
		chirp, err := scanChirp(rows)
		if err != nil {
			rows.Close()
			return nil, false, err
		}
		chirp.CanEdit = chirp.MemberID == viewer
		chirps = append(chirps, chirp)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, false, err
	}
	more := len(chirps) > chirpsPerPage
	if more {
		chirps = chirps[:chirpsPerPage]
	}
	for i := range chirps {
		chirps[i].Species, err = s.ChirpSpecies(ctx, chirps[i].ID)
		if err != nil {
			return nil, false, err
		}
	}
	return chirps, more, nil
}

// SaveChirp validates relational choices and saves the post and tags together.
// The returned old photo may be removed only after the transaction commits.
func (s *Store) SaveChirp(ctx context.Context, memberID int64, form ChirpForm, newPhoto string) (int64, string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, "", err
	}
	defer tx.Rollback()
	var oldPhoto string
	if form.ID != 0 {
		var owner int64
		if err := tx.QueryRowContext(ctx, `SELECT member_id, COALESCE(image_path, '') FROM chirps WHERE id = ?`, form.ID).Scan(&owner, &oldPhoto); err != nil {
			return 0, "", err
		}
		if owner != memberID {
			return 0, "", errChirpOwner
		}
	}
	photo := newPhoto
	if form.ID != 0 && newPhoto == "" && !form.RemovePhoto {
		// Keep the current database value, even if another edit replaced the photo
		// while this request was being parsed or decoded.
		photo = oldPhoto
	}
	var flock any
	if form.FlockID != "" {
		id, err := strconv.ParseInt(form.FlockID, 10, 64)
		if err != nil || id < 1 {
			return 0, "", errChirpFields
		}
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM flock_members WHERE flock_id = ? AND member_id = ?`, id, memberID).Scan(&count); err != nil {
			return 0, "", err
		}
		if count != 1 {
			return 0, "", errChirpFields
		}
		flock = id
	}
	if form.Visibility != "public" && form.Visibility != "flock" || form.Visibility == "flock" && flock == nil {
		return 0, "", errChirpFields
	}
	for id := range form.SelectedSpecies {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM species WHERE id = ?`, id).Scan(&count); err != nil {
			return 0, "", err
		}
		if count != 1 {
			return 0, "", errChirpFields
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	id := form.ID
	if id == 0 {
		result, err := tx.ExecContext(ctx, `INSERT INTO chirps(member_id, flock_id, content, image_path, visibility, created_at, updated_at) VALUES (?, ?, ?, NULLIF(?, ''), ?, ?, ?)`, memberID, flock, form.Content, photo, form.Visibility, now, now)
		if err != nil {
			return 0, "", err
		}
		id, err = result.LastInsertId()
		if err != nil {
			return 0, "", err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE chirps SET flock_id = ?, content = ?, image_path = NULLIF(?, ''), visibility = ?, updated_at = ? WHERE id = ?`, flock, form.Content, photo, form.Visibility, now, id); err != nil {
			return 0, "", err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM chirp_species WHERE chirp_id = ?`, id); err != nil {
			return 0, "", err
		}
	}
	for speciesID := range form.SelectedSpecies {
		if _, err := tx.ExecContext(ctx, `INSERT INTO chirp_species(chirp_id, species_id) VALUES (?, ?)`, id, speciesID); err != nil {
			return 0, "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, "", err
	}
	return id, oldPhoto, nil
}

func (s *Store) DeleteChirp(ctx context.Context, id, memberID int64) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var owner int64
	var photo string
	if err := tx.QueryRowContext(ctx, `SELECT member_id, COALESCE(image_path, '') FROM chirps WHERE id = ?`, id).Scan(&owner, &photo); err != nil {
		return "", err
	}
	if owner != memberID {
		return "", errChirpOwner
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM chirps WHERE id = ?`, id); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return photo, nil
}

func viewerID(c echo.Context) int64 {
	if member := currentMember(c); member != nil {
		return member.ID
	}
	return 0
}

func pageNumber(c echo.Context) (int, error) {
	if c.QueryParam("page") == "" {
		return 1, nil
	}
	page, err := strconv.Atoi(c.QueryParam("page"))
	if err != nil || page < 1 || page > 1000000 {
		return 0, echo.NewHTTPError(http.StatusBadRequest, "Choose a valid page number.")
	}
	return page, nil
}

func (a *application) feed(c echo.Context) error {
	page, err := pageNumber(c)
	if err != nil {
		return err
	}
	chirps, more, err := a.store.VisibleChirps(c.Request().Context(), viewerID(c), 0, page)
	if err != nil {
		return err
	}
	return a.render(c, http.StatusOK, "feed.html", pageData{Title: "Your birding community", Active: "/feed", Timeline: chirps, Page: page,
		PreviousURL: "/feed?page=" + strconv.Itoa(page-1), NextURL: "/feed?page=" + strconv.Itoa(page+1), HasMore: more})
}

func chirpID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 1 {
		return 0, echo.NewHTTPError(http.StatusNotFound, "This chirp could not be found.")
	}
	return id, nil
}

func (a *application) ownedChirp(c echo.Context, id int64) (Chirp, error) {
	chirp, err := a.store.Chirp(c.Request().Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		return Chirp{}, echo.NewHTTPError(http.StatusNotFound, "This chirp could not be found.")
	}
	if err != nil {
		return Chirp{}, err
	}
	if chirp.MemberID != viewerID(c) {
		return Chirp{}, echo.NewHTTPError(http.StatusForbidden, errChirpOwner.Error())
	}
	return chirp, nil
}

func (a *application) renderChirpForm(c echo.Context, status int, form ChirpForm, message string) error {
	birds, err := a.store.Species(c.Request().Context())
	if err != nil {
		return err
	}
	flocks, err := a.store.MemberFlocks(c.Request().Context(), viewerID(c))
	if err != nil {
		return err
	}
	title := "Write a chirp"
	if form.ID != 0 {
		title = "Edit your chirp"
	}
	return a.render(c, status, "chirp-form.html", pageData{Title: title, Active: "/feed", Form: form, Species: birds, Memberships: flocks, Error: message})
}

func (a *application) chirpForm(c echo.Context) error {
	if currentMember(c) == nil {
		return loginRedirect(c)
	}
	form := ChirpForm{Visibility: "public", SelectedSpecies: map[int64]bool{}}
	if raw := c.QueryParam("id"); raw != "" {
		id, err := chirpID(raw)
		if err != nil {
			return err
		}
		chirp, err := a.ownedChirp(c, id)
		if err != nil {
			return err
		}
		form.ID, form.Content, form.Visibility, form.ImagePath = id, chirp.Content, chirp.Visibility, chirp.ImagePath
		if chirp.FlockID.Valid {
			form.FlockID = strconv.FormatInt(chirp.FlockID.Int64, 10)
		}
		birds, err := a.store.ChirpSpecies(c.Request().Context(), id)
		if err != nil {
			return err
		}
		for _, bird := range birds {
			form.SelectedSpecies[bird.ID] = true
		}
	}
	return a.renderChirpForm(c, http.StatusOK, form, "")
}

func (a *application) saveChirp(c echo.Context) error {
	if currentMember(c) == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Sign in to write a chirp.")
	}
	form := ChirpForm{Content: strings.TrimSpace(c.FormValue("content")), Visibility: c.FormValue("visibility"), FlockID: c.FormValue("flock_id"), SelectedSpecies: map[int64]bool{}, RemovePhoto: c.FormValue("remove_photo") == "yes", PhotoPermission: c.FormValue("photo_permission") == "yes"}
	if raw := c.FormValue("id"); raw != "" {
		id, err := chirpID(raw)
		if err != nil {
			return err
		}
		chirp, err := a.ownedChirp(c, id)
		if err != nil {
			return err
		}
		form.ID, form.ImagePath = id, chirp.ImagePath
	}
	message := ""
	for _, raw := range c.Request().PostForm["species_id"] {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id < 1 {
			message = "Choose species from the catalog."
			continue
		}
		form.SelectedSpecies[id] = true
	}
	if !utf8.ValidString(form.Content) || utf8.RuneCountInString(form.Content) < 1 || utf8.RuneCountInString(form.Content) > 500 {
		message = "Write a chirp between 1 and 500 characters."
	}
	if form.Visibility != "public" && form.Visibility != "flock" {
		message = "Choose public or flock-only visibility."
	}
	if message != "" {
		return a.renderChirpForm(c, http.StatusUnprocessableEntity, form, message)
	}
	newPhoto, err := receivePhoto(c, form.PhotoPermission)
	if err != nil {
		var input *photoInputError
		if errors.As(err, &input) {
			return a.renderChirpForm(c, http.StatusUnprocessableEntity, form, input.Error())
		}
		return err
	}
	_, oldPhoto, err := a.store.SaveChirp(c.Request().Context(), viewerID(c), form, newPhoto)
	if err != nil {
		removePhoto(c, newPhoto)
		if errors.Is(err, errChirpFields) {
			return a.renderChirpForm(c, http.StatusUnprocessableEntity, form, err.Error())
		}
		if errors.Is(err, errChirpOwner) {
			return echo.NewHTTPError(http.StatusForbidden, err.Error())
		}
		if errors.Is(err, sql.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "This chirp could not be found.")
		}
		return err
	}
	if newPhoto != "" || form.RemovePhoto {
		removePhoto(c, oldPhoto)
	}
	return c.Redirect(http.StatusSeeOther, "/feed")
}

func (a *application) deleteChirp(c echo.Context) error {
	if currentMember(c) == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Sign in to manage your chirps.")
	}
	id, err := chirpID(c.Param("id"))
	if err != nil {
		return err
	}
	if _, err := a.ownedChirp(c, id); err != nil {
		return err
	}
	if c.FormValue("confirm_delete") != "yes" {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, "Confirm permanent chirp deletion before continuing.")
	}
	photo, err := a.store.DeleteChirp(c.Request().Context(), id, viewerID(c))
	if errors.Is(err, errChirpOwner) {
		return echo.NewHTTPError(http.StatusForbidden, err.Error())
	}
	if errors.Is(err, sql.ErrNoRows) {
		return echo.NewHTTPError(http.StatusNotFound, "This chirp could not be found.")
	}
	if err != nil {
		return err
	}
	removePhoto(c, photo)
	return c.Redirect(http.StatusSeeOther, "/feed")
}
