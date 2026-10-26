package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func profileAccount(t *testing.T, store *Store, name string) (*testBrowser, *Member) {
	t.Helper()
	b := newTestBrowser(t, store)
	csrf := csrfFrom(t, b.request("GET", "/login?tab=register", nil))
	requireStatus(t, b.request("POST", "/register", registration(csrf, name+"@example.com", name)), 303)
	session, err := store.Session(context.Background(), b.cookies[sessionCookie].Value)
	if err != nil {
		t.Fatal(err)
	}
	return b, &session.Member
}

func profilePath(member *Member) string { return fmt.Sprintf("/members/%d", member.ID) }

func profileValues(csrf string) url.Values {
	return url.Values{"csrf_token": {csrf}, "username": {" UPDATED_BIRDER "}, "display_name": {" River Birder "}, "region": {" Boise foothills "}, "bio": {"Watching for owls.\nAnd finches."}}
}

func execFixture(t *testing.T, store *Store, query string, args ...any) int64 {
	t.Helper()
	result, err := store.db.Exec(query, args...)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func fixtureFlock(t *testing.T, store *Store, owner *Member, name string) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	id := execFixture(t, store, `INSERT INTO flocks(name, region, topic, description, created_by, created_at) VALUES (?, 'Boise', 'Owls', 'A birding group', ?, ?)`, name, owner.ID, now)
	execFixture(t, store, `INSERT INTO flock_members(flock_id, member_id, role, joined_at) VALUES (?, ?, 'owner', ?)`, id, owner.ID, now)
	return id
}

func TestProfileDisplayAndInlineEditing(t *testing.T) {
	store := testStore(t)
	b, member := profileAccount(t, store, "profile_birder")
	path := profilePath(member)
	page := b.request("GET", path, nil)
	requireStatus(t, page, 200)
	for _, text := range []string{"No public chirps yet.", "No flock memberships yet.", "Edit profile", "Permanently delete account"} {
		if !strings.Contains(page.Body.String(), text) {
			t.Fatalf("profile missing %q", text)
		}
	}
	page = b.request("GET", path+"?edit=1", nil)
	requireStatus(t, page, 200)
	if !strings.Contains(page.Body.String(), `action="`+path+`"`) || !strings.Contains(page.Body.String(), `value="profile_birder"`) {
		t.Fatal("inline form not prefilled")
	}
	values := profileValues(csrfFrom(t, page))
	// IDs and private account data supplied by a caller cannot change ownership.
	values.Set("user_id", "999")
	values.Set("id", "999")
	values.Set("email", "changed@example.com")
	rec := b.request("POST", path, values)
	requireStatus(t, rec, 303)
	if rec.Header().Get("Location") != path {
		t.Fatal("save did not redirect to the same profile")
	}
	stored, err := store.Member(context.Background(), member.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Username != "updated_birder" || stored.DisplayName != "River Birder" || stored.Region != "Boise foothills" || stored.Bio != values.Get("bio") {
		t.Fatalf("profile did not persist: %+v", stored)
	}
	var email string
	if err := store.db.QueryRow("SELECT email FROM users JOIN members ON members.user_id = users.id WHERE members.id = ?", member.ID).Scan(&email); err != nil || email != "profile_birder@example.com" {
		t.Fatalf("edit changed private identity: %s %v", email, err)
	}
	// A new server restores the updated identity from the existing session.
	restored := newTestBrowser(t, store)
	restored.cookies[sessionCookie] = b.cookies[sessionCookie]
	page = restored.request("GET", path, nil)
	requireStatus(t, page, 200)
	if !strings.Contains(page.Body.String(), "@updated_birder") || strings.Contains(page.Body.String(), `id="display-name"`) {
		t.Fatal("profile save did not exit edit mode or refresh session identity")
	}
	// Merely opening and cancelling the editor does not mutate the profile.
	requireStatus(t, b.request("GET", path+"?edit=1", nil), 200)
	requireStatus(t, b.request("GET", path, nil), 200)
}

func TestProfileValidationAndEscaping(t *testing.T) {
	store := testStore(t)
	b, member := profileAccount(t, store, "validation_birder")
	_, other := profileAccount(t, store, "other_birder")
	path := profilePath(member)
	csrf := csrfFrom(t, b.request("GET", path+"?edit=1", nil))
	for _, tc := range []struct {
		field, value string
		status       int
	}{
		{"username", "bad name", 422},
		{"username", "ab", 422},
		{"username", strings.Repeat("x", 25), 422},
		{"username", strings.ToUpper(other.Username), 409},
		{"display_name", " \t ", 422},
		{"display_name", strings.Repeat("鳥", 81), 422},
		{"region", strings.Repeat("x", 101), 422},
		{"bio", strings.Repeat("x", 501), 422},
		{"bio", "\xff", 422},
	} {
		t.Run(tc.field+fmt.Sprint(tc.status)+fmt.Sprint(len(tc.value)), func(t *testing.T) {
			values := profileValues(csrf)
			values.Set(tc.field, tc.value)
			rec := b.request("POST", path, values)
			requireStatus(t, rec, tc.status)
			if !strings.Contains(rec.Body.String(), `id="display-name"`) || !strings.Contains(rec.Body.String(), `role="alert"`) {
				t.Fatal("validation lost inline editor or feedback")
			}
			stored, err := store.Member(context.Background(), member.ID)
			if err != nil || stored.DisplayName != member.DisplayName || stored.Username != member.Username || stored.Bio != "" {
				t.Fatalf("invalid edit mutated profile: %+v %v", stored, err)
			}
		})
	}
	values := profileValues(csrf)
	values.Set("display_name", strings.Repeat("鳥", 80))
	values.Set("region", strings.Repeat("鳥", 100))
	values.Set("bio", strings.Repeat("鳥", 500))
	requireStatus(t, b.request("POST", path, values), 303)
	values.Set("display_name", `<script>alert('profile')</script>`)
	values.Set("bio", `</textarea><script>alert('bio')</script>'); DROP TABLE users; --`)
	values.Set("region", "")
	requireStatus(t, b.request("POST", path, values), 303)
	for _, suffix := range []string{"", "?edit=1"} {
		html := b.request("GET", path+suffix, nil).Body.String()
		if strings.Contains(html, "<script>") || !strings.Contains(html, "&lt;script&gt;") {
			t.Fatal("profile content was not escaped")
		}
	}
	if err := store.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestProfilePublicChirpsAndMembershipPrivacy(t *testing.T) {
	store := testStore(t)
	ownerBrowser, owner := profileAccount(t, store, "public_birder")
	otherBrowser, other := profileAccount(t, store, "another_birder")
	flockID := fixtureFlock(t, store, owner, "Woodland watchers")
	now := time.Now().UTC().Format(time.RFC3339)
	execFixture(t, store, `INSERT INTO flock_members(flock_id, member_id, role, joined_at) VALUES (?, ?, 'member', ?)`, flockID, other.ID, now)
	for i := 1; i <= 12; i++ {
		execFixture(t, store, `INSERT INTO chirps(member_id, flock_id, content, visibility, created_at, updated_at) VALUES (?, ?, ?, 'public', ?, ?)`, owner.ID, flockID, fmt.Sprintf("observation-%02d <script>bad</script>", i), now, now)
	}
	execFixture(t, store, `INSERT INTO chirps(member_id, flock_id, content, visibility, created_at, updated_at) VALUES (?, ?, 'flock-only-secret', 'flock', ?, ?)`, owner.ID, flockID, now, now)
	execFixture(t, store, `INSERT INTO chirps(member_id, content, visibility, created_at, updated_at) VALUES (?, 'another-members-observation', 'public', ?, ?)`, other.ID, now, now)
	for _, browser := range []*testBrowser{newTestBrowser(t, store), otherBrowser, ownerBrowser} {
		page := browser.request("GET", profilePath(owner)+"?edit=1", nil)
		requireStatus(t, page, 200)
		html := page.Body.String()
		for _, secret := range []string{"flock-only-secret", "another-members-observation", "observation-01", "observation-02", "public_birder@example.com", "<script>bad"} {
			if strings.Contains(html, secret) {
				t.Fatalf("profile exposed %q", secret)
			}
		}
		for _, want := range []string{"observation-12", "observation-03", "Woodland watchers", fmt.Sprintf(`href="/flocks/%d"`, flockID), "Flock owner", "&lt;script&gt;"} {
			if !strings.Contains(html, want) {
				t.Fatalf("profile missing %q", want)
			}
		}
		if strings.Index(html, "observation-12") > strings.Index(html, "observation-11") {
			t.Fatal("chirps not newest first")
		}
		if browser != ownerBrowser && (strings.Contains(html, `id="display-name"`) || strings.Contains(html, "Delete my account permanently") || strings.Contains(html, ">Edit profile<")) {
			t.Fatal("visitor sees owner controls")
		}
	}
}

func TestProfileUnauthorizedWrites(t *testing.T) {
	store := testStore(t)
	ownerBrowser, owner := profileAccount(t, store, "permission_owner")
	attacker, other := profileAccount(t, store, "permission_other")
	guest := newTestBrowser(t, store)
	guestCSRF := csrfFrom(t, guest.request("GET", "/login", nil))
	attackerCSRF := csrfFrom(t, attacker.request("GET", profilePath(other), nil))
	ownerCSRF := csrfFrom(t, ownerBrowser.request("GET", profilePath(owner), nil))
	for _, suffix := range []string{"", "/delete"} {
		path := profilePath(owner) + suffix
		values := profileValues(guestCSRF)
		values.Set("confirm_delete", "yes")
		requireStatus(t, guest.request("POST", path, values), 401)
		values.Set("csrf_token", attackerCSRF)
		requireStatus(t, attacker.request("POST", path, values), 403)
		values.Set("csrf_token", "forged")
		requireStatus(t, ownerBrowser.request("POST", path, values), 403)
		values.Del("csrf_token")
		requireStatus(t, ownerBrowser.request("POST", path, values), 403)
		values.Set("csrf_token", attackerCSRF)
		requireStatus(t, ownerBrowser.request("POST", path, values), 403)
	}
	requireStatus(t, ownerBrowser.request("GET", profilePath(owner)+"/delete", nil), 405)
	for _, id := range []string{"0", "-1", "nope", "999999", "9223372036854775808"} {
		for _, suffix := range []string{"", "/delete"} {
			requireStatus(t, ownerBrowser.request("POST", "/members/"+id+suffix, profileValues(ownerCSRF)), 404)
		}
	}
	stored, err := store.Member(context.Background(), owner.ID)
	if err != nil || stored.Username != owner.Username {
		t.Fatal("unauthorized request changed owner")
	}
	execFixture(t, store, `UPDATE sessions SET expires_at = ?`, time.Now().UTC().Add(-time.Hour).Format(time.RFC3339))
	values := profileValues(ownerCSRF)
	values.Set("confirm_delete", "yes")
	requireStatus(t, ownerBrowser.request("POST", profilePath(owner)+"/delete", values), 403)
	if _, err := store.Member(context.Background(), owner.ID); err != nil {
		t.Fatal("expired session deleted account")
	}
}

func TestAccountDeletionCascadesAndRevokesEverySession(t *testing.T) {
	store := testStore(t)
	b, member := profileAccount(t, store, "delete_birder")
	_, flockOwner := profileAccount(t, store, "retained_birder")
	flockID := fixtureFlock(t, store, flockOwner, "Surviving flock")
	now := time.Now().UTC().Format(time.RFC3339)
	execFixture(t, store, `INSERT INTO flock_members(flock_id, member_id, role, joined_at) VALUES (?, ?, 'member', ?)`, flockID, member.ID, now)
	uploadDir := t.TempDir()
	t.Setenv("UPLOAD_DIR", uploadDir)
	for _, visibility := range []string{"public", "flock"} {
		photo := visibility + ".png"
		if err := os.WriteFile(filepath.Join(uploadDir, photo), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
		id := execFixture(t, store, `INSERT INTO chirps(member_id, flock_id, content, image_path, visibility, created_at, updated_at) VALUES (?, ?, 'Delete this chirp', ?, ?, ?, ?)`, member.ID, flockID, photo, visibility, now, now)
		execFixture(t, store, `INSERT INTO chirp_species(chirp_id, species_id) SELECT ?, id FROM species ORDER BY id LIMIT 1`, id)
	}
	retainedID := execFixture(t, store, `INSERT INTO chirps(member_id, content, visibility, created_at, updated_at) VALUES (?, 'Retain this chirp', 'public', ?, ?)`, flockOwner.ID, now, now)
	var userID int64
	if err := store.db.QueryRow("SELECT user_id FROM members WHERE id = ?", member.ID).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	secondToken, err := store.CreateSession(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	firstToken := b.cookies[sessionCookie].Value
	csrf := csrfFrom(t, b.request("GET", profilePath(member), nil))
	for _, confirmation := range []string{"", "no", "true"} {
		requireStatus(t, b.request("POST", profilePath(member)+"/delete", url.Values{"csrf_token": {csrf}, "confirm_delete": {confirmation}}), 422)
		if _, err := store.Member(context.Background(), member.ID); err != nil {
			t.Fatal("unconfirmed deletion changed account")
		}
	}
	rec := b.request("POST", profilePath(member)+"/delete", url.Values{"csrf_token": {csrf}, "confirm_delete": {"yes"}})
	requireStatus(t, rec, 303)
	if rec.Header().Get("Location") != "/" || b.cookies[sessionCookie] != nil || b.cookies[csrfCookie] != nil {
		t.Fatal("deletion did not redirect home and clear cookies")
	}
	for _, table := range []string{"users", "members", "sessions", "flock_members", "chirps", "chirp_species"} {
		var count int
		// Only the retained owner/account/membership/chirp remain; that chirp has no tags.
		want := 1
		if table == "chirp_species" {
			want = 0
		}
		if err := store.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != want {
			t.Fatalf("unexpected %s count %d, want %d: %v", table, count, want, err)
		}
	}
	for _, token := range []string{firstToken, secondToken} {
		if _, err := store.Session(context.Background(), token); err != sql.ErrNoRows {
			t.Fatalf("session survived deletion: %v", err)
		}
	}
	for _, photo := range []string{"public.png", "flock.png"} {
		if _, err := os.Stat(filepath.Join(uploadDir, photo)); !os.IsNotExist(err) {
			t.Fatalf("deleted account photo remains: %s", photo)
		}
	}
	if _, err := store.Authenticate(context.Background(), "delete_birder@example.com", "birdwatching-passphrase"); err != sql.ErrNoRows {
		t.Fatal("deleted account can authenticate")
	}
	requireStatus(t, b.request("GET", profilePath(member), nil), 404)
	requireStatus(t, b.request("GET", "/me", nil), 303)
	var survivingID int64
	if err := store.db.QueryRow("SELECT id FROM chirps").Scan(&survivingID); err != nil || survivingID != retainedID {
		t.Fatal("deletion changed another member's chirp")
	}
	rows, err := store.db.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() || rows.Err() != nil {
		t.Fatal("deletion left broken foreign keys")
	}
}

func TestFlockOwnerDeletionRejectedAndTransferDependency(t *testing.T) {
	store := testStore(t)
	b, creator := profileAccount(t, store, "flock_creator")
	newOwnerBrowser, successor := profileAccount(t, store, "flock_successor")
	first := fixtureFlock(t, store, creator, "First flock")
	second := fixtureFlock(t, store, creator, "Second flock")
	csrf := csrfFrom(t, b.request("GET", profilePath(creator), nil))
	values := url.Values{"csrf_token": {csrf}, "confirm_delete": {"yes"}}
	blocked := b.request("POST", profilePath(creator)+"/delete", values)
	requireStatus(t, blocked, 409)
	if !strings.Contains(blocked.Body.String(), errFlockOwner.Error()) {
		t.Fatal("owner rejection missing explanation")
	}
	if _, err := store.Session(context.Background(), b.cookies[sessionCookie].Value); err != nil {
		t.Fatal("rejection revoked owner session")
	}
	// Fixture transfer models the Checkpoint 5 UI dependency; role, rather than
	// original creator, remains the authoritative ownership field.
	now := time.Now().UTC().Format(time.RFC3339)
	for _, flock := range []int64{first, second} {
		execFixture(t, store, `UPDATE flock_members SET role = 'member' WHERE flock_id = ?`, flock)
		execFixture(t, store, `INSERT INTO flock_members(flock_id, member_id, role, joined_at) VALUES (?, ?, 'owner', ?)`, flock, successor.ID, now)
		if flock == first {
			requireStatus(t, b.request("POST", profilePath(creator)+"/delete", values), 409)
		}
	}
	// The successor did not create either flock but must still be blocked.
	successorCSRF := csrfFrom(t, newOwnerBrowser.request("GET", profilePath(successor), nil))
	requireStatus(t, newOwnerBrowser.request("POST", profilePath(successor)+"/delete", url.Values{"csrf_token": {successorCSRF}, "confirm_delete": {"yes"}}), 409)
	requireStatus(t, b.request("POST", profilePath(creator)+"/delete", values), 303)
	var count int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM flocks WHERE created_by = ?", successor.ID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("creator references not repaired: %d %v", count, err)
	}
	if _, err := store.Member(context.Background(), successor.ID); err != nil {
		t.Fatal("deletion removed successor")
	}
}

func TestAccountDeletionRollsBackOnDatabaseFailure(t *testing.T) {
	store := testStore(t)
	b, creator := profileAccount(t, store, "rollback_creator")
	_, successor := profileAccount(t, store, "rollback_successor")
	flock := fixtureFlock(t, store, successor, "Transferred flock")
	execFixture(t, store, "UPDATE flocks SET created_by = ? WHERE id = ?", creator.ID, flock)
	execFixture(t, store, `CREATE TRIGGER reject_deletion BEFORE DELETE ON users BEGIN SELECT RAISE(ABORT, 'fixture failure'); END`)
	csrf := csrfFrom(t, b.request("GET", profilePath(creator), nil))
	requireStatus(t, b.request("POST", profilePath(creator)+"/delete", url.Values{"csrf_token": {csrf}, "confirm_delete": {"yes"}}), 500)
	var createdBy int64
	if err := store.db.QueryRow("SELECT created_by FROM flocks WHERE id = ?", flock).Scan(&createdBy); err != nil || createdBy != creator.ID {
		t.Fatal("failed deletion committed creator repair")
	}
	if _, err := store.Session(context.Background(), b.cookies[sessionCookie].Value); err != nil {
		t.Fatal("failed deletion revoked session")
	}
}

func TestProfileEditsSurviveDatabaseReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "persisted.db")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Initialize(); err != nil {
		t.Fatal(err)
	}
	b, member := profileAccount(t, store, "restart_birder")
	csrf := csrfFrom(t, b.request("GET", profilePath(member)+"?edit=1", nil))
	requireStatus(t, b.request("POST", profilePath(member), profileValues(csrf)), 303)
	cookie := b.cookies[sessionCookie]
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Initialize(); err != nil {
		t.Fatal(err)
	}
	b = newTestBrowser(t, store)
	b.cookies[sessionCookie] = cookie
	page := b.request("GET", profilePath(member), nil)
	requireStatus(t, page, 200)
	if !strings.Contains(page.Body.String(), "River Birder") || !strings.Contains(page.Body.String(), ">Edit profile<") {
		t.Fatal("profile or session did not survive database reopen")
	}
}

func TestAccountDeletionRejectsOwnerlessCreatorReference(t *testing.T) {
	store := testStore(t)
	b, member := profileAccount(t, store, "legacy_creator")
	flock := fixtureFlock(t, store, member, "Incomplete legacy flock")
	execFixture(t, store, "DELETE FROM flock_members WHERE flock_id = ?", flock)
	csrf := csrfFrom(t, b.request("GET", profilePath(member), nil))
	values := url.Values{"csrf_token": {csrf}, "confirm_delete": {"yes"}}
	requireStatus(t, b.request("POST", profilePath(member)+"/delete", values), 409)
	if _, err := store.Member(context.Background(), member.ID); err != nil {
		t.Fatal("ownerless flock reference allowed account removal")
	}
	// Removing the flock resolves the ownership dependency and permits deletion.
	execFixture(t, store, "DELETE FROM flocks WHERE id = ?", flock)
	requireStatus(t, b.request("POST", profilePath(member)+"/delete", values), 303)
}

func TestAccountPhotoCleanupStaysInsideUploadDirectory(t *testing.T) {
	store := testStore(t)
	b, member := profileAccount(t, store, "photo_cleanup")
	root := t.TempDir()
	uploadDir := filepath.Join(root, "uploads")
	if err := os.Mkdir(uploadDir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UPLOAD_DIR", uploadDir)
	sentinel := filepath.Join(root, "keep.png")
	if err := os.WriteFile(sentinel, []byte("retain"), 0600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, path := range []string{"../keep.png", sentinel, `..\keep.png`, "missing.png", "", ".", ".."} {
		execFixture(t, store, `INSERT INTO chirps(member_id, content, image_path, visibility, created_at, updated_at) VALUES (?, 'legacy path', ?, 'public', ?, ?)`, member.ID, path, now, now)
	}
	csrf := csrfFrom(t, b.request("GET", profilePath(member), nil))
	requireStatus(t, b.request("POST", profilePath(member)+"/delete", url.Values{"csrf_token": {csrf}, "confirm_delete": {"yes"}}), 303)
	if contents, err := os.ReadFile(sentinel); err != nil || string(contents) != "retain" {
		t.Fatalf("cleanup escaped upload directory: %v", err)
	}
	if _, err := os.Stat(uploadDir); err != nil {
		t.Fatal("cleanup removed upload directory")
	}
}
