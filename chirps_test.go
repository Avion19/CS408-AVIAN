package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func chirpValues(csrf, content string) url.Values {
	return url.Values{"csrf_token": {csrf}, "content": {content}, "visibility": {"public"}, "species_id": {"1", "2"}}
}

func lastChirp(t *testing.T, store *Store) Chirp {
	t.Helper()
	var id int64
	if err := store.db.QueryRow(`SELECT MAX(id) FROM chirps`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	chirp, err := store.Chirp(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return chirp
}

func TestChirpLifecycleAndPersistence(t *testing.T) {
	t.Setenv("UPLOAD_DIR", t.TempDir())
	path := filepath.Join(t.TempDir(), "avian.db")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	if err := store.Initialize(); err != nil {
		t.Fatal(err)
	}
	owner, member := profileAccount(t, store, "chirp_author")
	page := owner.request("GET", "/chirps/form", nil)
	requireStatus(t, page, 200)
	csrf := csrfFrom(t, page)
	content := "A robin <script>alert('bird')</script> & a finch."
	values := chirpValues(csrf, content)
	values["species_id"] = []string{"1", "2", "1"} // Repeated tags become one association.
	values.Set("member_id", "9999")
	values.Set("photo_permission", "yes")
	result := multipartChirp(t, owner, values, testUpload{name: "persist.jpg", data: testPhoto(t, "jpeg", 8, 6)})
	requireStatus(t, result, 303)
	if result.Header().Get("Location") != "/feed" {
		t.Fatal("create did not redirect to Feed")
	}
	chirp := lastChirp(t, store)
	if chirp.MemberID != member.ID || chirp.Content != content || chirp.Visibility != "public" {
		t.Fatalf("unexpected chirp: %+v", chirp)
	}
	tags, err := store.ChirpSpecies(context.Background(), chirp.ID)
	if err != nil || len(tags) != 2 {
		t.Fatalf("tags not saved: %+v %v", tags, err)
	}
	page = owner.request("GET", "/feed", nil)
	requireStatus(t, page, 200)
	if strings.Contains(page.Body.String(), "<script>") || !strings.Contains(page.Body.String(), "&lt;script&gt;") || !strings.Contains(page.Body.String(), fmt.Sprintf("/chirps/form?id=%d", chirp.ID)) || !strings.Contains(page.Body.String(), "/species?selected=1") {
		t.Fatal("Feed did not escape text or link author controls/tags")
	}
	editURL := fmt.Sprintf("/chirps/form?id=%d", chirp.ID)
	page = owner.request("GET", editURL, nil)
	requireStatus(t, page, 200)
	if !strings.Contains(page.Body.String(), `value="1" checked`) || !strings.Contains(page.Body.String(), "&lt;script&gt;") {
		t.Fatal("editor did not retain content and tags")
	}
	values = chirpValues(csrf, strings.Repeat("鳥", 500))
	values.Set("id", fmt.Sprint(chirp.ID))
	values["species_id"] = []string{"3"}
	requireStatus(t, owner.request("POST", "/chirps/form", values), 303)
	cookie := owner.cookies[sessionCookie]
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Initialize(); err != nil {
		t.Fatal(err)
	}
	restored := newTestBrowser(t, store)
	restored.cookies[sessionCookie] = cookie
	chirp, err = store.Chirp(context.Background(), chirp.ID)
	if err != nil || chirp.Content != values.Get("content") {
		t.Fatalf("post lost after restart: %+v %v", chirp, err)
	}
	tags, err = store.ChirpSpecies(context.Background(), chirp.ID)
	if err != nil || len(tags) != 1 || tags[0].ID != 3 {
		t.Fatalf("edited tags lost after restart: %+v %v", tags, err)
	}
	requireStatus(t, restored.request("GET", editURL, nil), 200)
	requireStatus(t, restored.request("GET", fmt.Sprintf("/chirps/%d/photo", chirp.ID), nil), 200)
	deleteURL := fmt.Sprintf("/chirps/%d/delete", chirp.ID)
	requireStatus(t, restored.request("GET", deleteURL, nil), 405)
	requireStatus(t, restored.request("POST", deleteURL, url.Values{"csrf_token": {csrf}}), 422)
	requireStatus(t, restored.request("POST", deleteURL, url.Values{"csrf_token": {csrf}, "confirm_delete": {"yes"}}), 303)
	if _, err := store.Chirp(context.Background(), chirp.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("deleted post retained: %v", err)
	}
	tags, err = store.ChirpSpecies(context.Background(), chirp.ID)
	if err != nil || len(tags) != 0 {
		t.Fatal("deleted post retained tags")
	}
}

func TestChirpValidationAndPermissions(t *testing.T) {
	store := testStore(t)
	owner, member := profileAccount(t, store, "validate_chirp")
	other, otherMember := profileAccount(t, store, "other_chirp")
	flock := fixtureFlock(t, store, member, "Member's flock")
	otherFlock := fixtureFlock(t, store, otherMember, "Other flock")
	csrf := csrfFrom(t, owner.request("GET", "/chirps/form", nil))
	for _, tc := range []struct{ name, field, value string }{
		{"blank", "content", " \t"}, {"overlong", "content", strings.Repeat("鳥", 501)}, {"invalid UTF-8", "content", "\xff"},
		{"invalid visibility", "visibility", "secret"}, {"missing flock", "visibility", "flock"}, {"not a member", "flock_id", fmt.Sprint(otherFlock)},
		{"missing flock ID", "flock_id", "99999"}, {"SQL flock", "flock_id", "1 OR 1=1"}, {"missing species", "species_id", "99999"}, {"SQL species", "species_id", "1 OR 1=1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := chirpValues(csrf, "Keep my observation.")
			values.Set(tc.field, tc.value)
			page := owner.request("POST", "/chirps/form", values)
			requireStatus(t, page, 422)
			if !strings.Contains(page.Body.String(), `role="alert"`) || !strings.Contains(page.Body.String(), `enctype="multipart/form-data"`) {
				t.Fatal("validation lost the form")
			}
			if tc.field != "content" && !strings.Contains(page.Body.String(), "Keep my observation.") {
				t.Fatal("validation lost entered text")
			}
			var count int
			if err := store.db.QueryRow(`SELECT COUNT(*) FROM chirps`).Scan(&count); err != nil || count != 0 {
				t.Fatal("invalid create persisted data")
			}
		})
	}
	values := chirpValues(csrf, "Only my flock sees this.")
	values.Set("visibility", "flock")
	values.Set("flock_id", fmt.Sprint(flock))
	requireStatus(t, owner.request("POST", "/chirps/form", values), 303)
	chirp := lastChirp(t, store)
	otherCSRF := csrfFrom(t, other.request("GET", "/chirps/form", nil))
	values.Set("id", fmt.Sprint(chirp.ID))
	values.Set("csrf_token", otherCSRF)
	requireStatus(t, other.request("GET", fmt.Sprintf("/chirps/form?id=%d", chirp.ID), nil), 403)
	requireStatus(t, other.request("POST", "/chirps/form", values), 403)
	deleteURL := fmt.Sprintf("/chirps/%d/delete", chirp.ID)
	requireStatus(t, other.request("POST", deleteURL, url.Values{"csrf_token": {otherCSRF}, "confirm_delete": {"yes"}}), 403)
	guest := newTestBrowser(t, store)
	requireStatus(t, guest.request("GET", "/chirps/form?id=1", nil), 303)
	guestCSRF := csrfFrom(t, guest.request("GET", "/login", nil))
	requireStatus(t, guest.request("POST", "/chirps/form", chirpValues(guestCSRF, "Guest post")), 401)
	requireStatus(t, guest.request("POST", deleteURL, url.Values{"csrf_token": {guestCSRF}, "confirm_delete": {"yes"}}), 401)
	values.Set("csrf_token", csrf)
	requireStatus(t, other.request("POST", "/chirps/form", values), 403)
	requireStatus(t, owner.request("POST", "/chirps/form", chirpValues("", "No CSRF")), 403)
	for _, raw := range []string{"0", "-1", "999999", "bad", "9223372036854775808"} {
		requireStatus(t, owner.request("GET", "/chirps/form?id="+raw, nil), 404)
		bad := chirpValues(csrf, "Missing post")
		bad.Set("id", raw)
		requireStatus(t, owner.request("POST", "/chirps/form", bad), 404)
		requireStatus(t, owner.request("POST", "/chirps/"+raw+"/delete", url.Values{"csrf_token": {csrf}, "confirm_delete": {"yes"}}), 404)
	}
	// An invalid edit must preserve both the old content and tags.
	values.Set("csrf_token", csrf)
	values.Set("species_id", "99999")
	values.Set("content", "Should never be saved")
	requireStatus(t, owner.request("POST", "/chirps/form", values), 422)
	saved, err := store.Chirp(context.Background(), chirp.ID)
	tags, tagErr := store.ChirpSpecies(context.Background(), chirp.ID)
	if err != nil || tagErr != nil || saved.Content != chirp.Content || len(tags) != 2 {
		t.Fatal("invalid edit changed persisted chirp or tags")
	}
	execFixture(t, store, `UPDATE sessions SET expires_at = '2000-01-01T00:00:00Z' WHERE user_id = (SELECT user_id FROM members WHERE id = ?)`, member.ID)
	requireStatus(t, owner.request("POST", "/chirps/form", chirpValues(csrf, "Expired")), 403)
}

func TestFeedSpeciesVisibilityAndPagination(t *testing.T) {
	store := testStore(t)
	owner, member := profileAccount(t, store, "timeline_owner")
	eligible, eligibleMember := profileAccount(t, store, "timeline_member")
	outsider, _ := profileAccount(t, store, "timeline_outsider")
	guest := newTestBrowser(t, store)
	flock := fixtureFlock(t, store, member, "Private observations")
	execFixture(t, store, `INSERT INTO flock_members(flock_id, member_id, role, joined_at) VALUES (?, ?, 'member', ?)`, flock, eligibleMember.ID, time.Now().UTC().Format(time.RFC3339))
	for i := 0; i < chirpsPerPage+3; i++ {
		form := ChirpForm{Content: fmt.Sprintf("public-observation-%02d", i), Visibility: "public", SelectedSpecies: map[int64]bool{1: true, 2: true}}
		if _, _, err := store.SaveChirp(context.Background(), member.ID, form, ""); err != nil {
			t.Fatal(err)
		}
	}
	hidden, _, err := store.SaveChirp(context.Background(), member.ID, ChirpForm{Content: "private-observation-marker", Visibility: "flock", FlockID: fmt.Sprint(flock), SelectedSpecies: map[int64]bool{1: true}}, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, browser := range []*testBrowser{guest, outsider} {
		for _, route := range []string{"/feed", "/feed?page=2", "/species?selected=1", "/species?selected=1&page=2"} {
			page := browser.request("GET", route, nil)
			requireStatus(t, page, 200)
			if strings.Contains(page.Body.String(), "private-observation-marker") || strings.Contains(page.Body.String(), "Edit chirp") {
				t.Fatalf("restricted content or controls leaked via %s", route)
			}
		}
	}
	for _, browser := range []*testBrowser{owner, eligible} {
		for _, route := range []string{"/feed", "/species?selected=1"} {
			page := browser.request("GET", route, nil)
			requireStatus(t, page, 200)
			if !strings.Contains(page.Body.String(), "private-observation-marker") {
				t.Fatalf("member missing eligible chirp on %s", route)
			}
			if browser == eligible && strings.Contains(page.Body.String(), "Edit chirp") {
				t.Fatal("member got another author's controls")
			}
		}
	}
	page1, page2 := guest.request("GET", "/feed", nil), guest.request("GET", "/feed?page=2", nil)
	if strings.Count(page1.Body.String(), `id="chirp-`) != 20 || strings.Count(page2.Body.String(), `id="chirp-`) != 3 {
		t.Fatal("pagination returned incorrect page sizes")
	}
	if !strings.Contains(page1.Body.String(), "Older chirps") || strings.Contains(page2.Body.String(), "Older chirps") || !strings.Contains(page2.Body.String(), "Newer chirps") {
		t.Fatal("incorrect page controls")
	}
	// All timestamps tie, so descending IDs must give stable, non-overlapping pages.
	if strings.Contains(page1.Body.String(), "public-observation-00") || !strings.Contains(page2.Body.String(), "public-observation-00") {
		t.Fatal("pagination did not order newest first")
	}
	none := guest.request("GET", "/species?selected=3", nil)
	if strings.Contains(none.Body.String(), "public-observation-") || !strings.Contains(none.Body.String(), "No observations to show") {
		t.Fatal("species filter returned untagged chirps")
	}
	speciesPage := guest.request("GET", "/species?selected=1&q=finch", nil)
	link := regexp.MustCompile(`href="([^"]+)">Older chirps`).FindStringSubmatch(speciesPage.Body.String())
	if len(link) != 2 {
		t.Fatal("missing species pagination link")
	}
	olderURL := html.UnescapeString(link[1])
	parsed, err := url.Parse(olderURL)
	if err != nil || parsed.Path != "/species" || parsed.Query().Get("selected") != "1" || parsed.Query().Get("q") != "finch" || parsed.Query().Get("page") != "2" {
		t.Fatal("species pagination lost selected bird or search")
	}
	olderPage := guest.request("GET", olderURL, nil)
	requireStatus(t, olderPage, 200)
	if strings.Count(olderPage.Body.String(), `id="chirp-`) != 3 {
		t.Fatal("following species pagination did not load the next page")
	}
	execFixture(t, store, `DELETE FROM flock_members WHERE flock_id = ? AND member_id = ?`, flock, eligibleMember.ID)
	if _, err := store.VisibleChirp(context.Background(), hidden, eligibleMember.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("former member retained access")
	}
	if strings.Contains(eligible.request("GET", "/feed", nil).Body.String(), "private-observation-marker") {
		t.Fatal("Feed leaked chirp after membership removal")
	}
	for _, raw := range []string{"0", "-1", "bad", "1000001", "9223372036854775808"} {
		requireStatus(t, guest.request("GET", "/feed?page="+raw, nil), 400)
	}
}

func TestChirpTransactionRollback(t *testing.T) {
	store := testStore(t)
	_, member := profileAccount(t, store, "atomic_chirp")
	form := ChirpForm{Content: "Original", Visibility: "public", SelectedSpecies: map[int64]bool{1: true}}
	id, _, err := store.SaveChirp(context.Background(), member.ID, form, "original.jpg")
	if err != nil {
		t.Fatal(err)
	}
	execFixture(t, store, `CREATE TRIGGER fail_tag BEFORE INSERT ON chirp_species BEGIN SELECT RAISE(ABORT, 'fixture failure'); END`)
	form.ID, form.Content, form.SelectedSpecies = id, "Changed", map[int64]bool{2: true}
	if _, _, err := store.SaveChirp(context.Background(), member.ID, form, "replacement.jpg"); err == nil {
		t.Fatal("fixture did not fail")
	}
	saved, err := store.Chirp(context.Background(), id)
	tags, tagErr := store.ChirpSpecies(context.Background(), id)
	if err != nil || tagErr != nil || saved.Content != "Original" || saved.ImagePath != "original.jpg" || len(tags) != 1 || tags[0].ID != 1 {
		t.Fatal("edit failure did not roll back entire transaction")
	}
	form.ID = 0
	if _, _, err := store.SaveChirp(context.Background(), member.ID, form, "new.jpg"); err == nil {
		t.Fatal("create fixture did not fail")
	}
	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM chirps`).Scan(&count); err != nil || count != 1 {
		t.Fatal("create failure left partial post")
	}
}

func TestSpeciesCatalogPhotosSearchAndUpgrade(t *testing.T) {
	store := testStore(t)
	guest := newTestBrowser(t, store)
	birds, err := store.Species(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, bird := range birds {
		if bird.ImagePath == "" || bird.ImageSourceURL == "" || bird.ImageAuthor == "" || bird.ImageLicense == "" || bird.ImageLicenseURL == "" {
			t.Fatalf("missing photo or credit: %+v", bird)
		}
		path := "." + bird.ImagePath
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
		page := guest.request("GET", fmt.Sprintf("/species?selected=%d", bird.ID), nil)
		requireStatus(t, page, 200)
		if !strings.Contains(page.Body.String(), bird.ImagePath) || !strings.Contains(page.Body.String(), bird.ImageLicenseURL) || !strings.Contains(page.Body.String(), bird.SourceURL) {
			t.Fatal("selected species missing reference photo or attribution")
		}
		requireStatus(t, guest.request("GET", bird.ImagePath, nil), 200)
	}
	for _, query := range []string{"ROBIN", "turdus"} {
		page := guest.request("GET", "/species?q="+query, nil)
		requireStatus(t, page, 200)
		if !strings.Contains(page.Body.String(), "American Robin") || strings.Contains(page.Body.String(), "American Goldfinch") {
			t.Fatal("species name search failed")
		}
	}
	page := guest.request("GET", "/species?q=%3Cscript%3E", nil)
	if strings.Contains(page.Body.String(), "<script>") || !strings.Contains(page.Body.String(), "No species match") {
		t.Fatal("empty search or escaping failed")
	}
	for _, raw := range []string{"0", "-1", "bad", "99999"} {
		requireStatus(t, guest.request("GET", "/species?selected="+raw, nil), 404)
	}
	// Simulate a pre-checkpoint catalog row awaiting reference media.
	execFixture(t, store, `UPDATE species SET description = 'Retain custom description', image_path = NULL WHERE id = 1`)
	if err := store.Initialize(); err != nil {
		t.Fatal(err)
	}
	birds, err = store.Species(context.Background())
	if err != nil || birds[0].ImagePath == "" || birds[0].Description != "Retain custom description" {
		t.Fatal("photo upgrade overwrote catalog text or failed to backfill")
	}
	if err := store.Initialize(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM species`).Scan(&count); err != nil || count != 12 {
		t.Fatal("seeding was not repeatable")
	}
}
