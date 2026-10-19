package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"
)

type testBrowser struct {
	server  *echo.Echo
	cookies map[string]*http.Cookie
}

func newTestBrowser(t *testing.T, store *Store) *testBrowser {
	t.Helper()
	server, err := newServer(store)
	if err != nil {
		t.Fatal(err)
	}
	return &testBrowser{server: server, cookies: map[string]*http.Cookie{}}
}
func (b *testBrowser) request(method, path string, values url.Values) *httptest.ResponseRecorder {
	var body *strings.Reader
	if values == nil {
		body = strings.NewReader("")
	} else {
		body = strings.NewReader(values.Encode())
	}
	req := httptest.NewRequest(method, path, body)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for _, cookie := range b.cookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	b.server.ServeHTTP(rec, req)
	for _, cookie := range rec.Result().Cookies() {
		if cookie.MaxAge < 0 {
			delete(b.cookies, cookie.Name)
		} else {
			b.cookies[cookie.Name] = cookie
		}
	}
	return rec
}

var csrfPattern = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

func csrfFrom(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	match := csrfPattern.FindStringSubmatch(rec.Body.String())
	if len(match) != 2 {
		t.Fatal("no CSRF field in response")
	}
	return match[1]
}
func testStore(t *testing.T) *Store {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "avian.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if err := store.Initialize(); err != nil {
		t.Fatal(err)
	}
	return store
}
func registration(csrf, email, username string) url.Values {
	return url.Values{"csrf_token": {csrf}, "email": {email}, "username": {username}, "password": {"birdwatching-passphrase"}, "confirm_password": {"birdwatching-passphrase"}, "next": {"/chirps/form?draft=1"}}
}
func requireStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status %d, want %d: %s", rec.Code, want, rec.Body.String())
	}
}

func TestAccountLifecycle(t *testing.T) {
	store := testStore(t)
	b := newTestBrowser(t, store)
	page := b.request("GET", "/login?tab=register", nil)
	requireStatus(t, page, 200)
	csrf := csrfFrom(t, page)
	result := b.request("POST", "/register", registration(csrf, " BIRDER@EXAMPLE.COM ", "Robin_19"))
	requireStatus(t, result, 303)
	if got := result.Header().Get("Location"); got != "/chirps/form?draft=1" {
		t.Fatalf("return path = %s", got)
	}
	cookie := b.cookies[sessionCookie]
	if cookie == nil || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" {
		t.Fatalf("unsafe session cookie: %+v", cookie)
	}
	var passwordHash, storedToken, email, username string
	if err := store.db.QueryRow(`SELECT u.email,u.password_hash,m.username,s.token_hash FROM users u JOIN members m ON m.user_id=u.id JOIN sessions s ON s.user_id=u.id`).Scan(&email, &passwordHash, &username, &storedToken); err != nil {
		t.Fatal(err)
	}
	if email != "birder@example.com" || username != "robin_19" {
		t.Fatalf("identity not normalized: %s %s", email, username)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte("birdwatching-passphrase")); err != nil {
		t.Fatal("password was not bcrypt hashed")
	}
	if storedToken == cookie.Value || storedToken != tokenHash(cookie.Value) {
		t.Fatal("session token not hashed")
	}
	page = b.request("GET", "/", nil)
	if !strings.Contains(page.Body.String(), "@robin_19") {
		t.Fatal("session did not restore member")
	}
	signedInCSRF := csrfFrom(t, page)
	if signedInCSRF == csrf {
		t.Fatal("CSRF did not rotate on login")
	}
	requireStatus(t, b.request("POST", "/logout", url.Values{"csrf_token": {csrf}}), 403)
	requireStatus(t, b.request("GET", "/logout", nil), 405)
	if b.cookies[sessionCookie] == nil {
		t.Fatal("GET logged the member out")
	}
	// Sessions survive constructing a new server, as they are persisted in SQLite.
	restored := newTestBrowser(t, store)
	restored.cookies[sessionCookie] = cookie
	requireStatus(t, restored.request("GET", "/chirps/form", nil), 200)
	requireStatus(t, b.request("POST", "/logout", url.Values{"csrf_token": {signedInCSRF}}), 303)
	if b.cookies[sessionCookie] != nil {
		t.Fatal("logout retained cookie")
	}
	if _, err := store.Session(context.Background(), cookie.Value); err != sql.ErrNoRows {
		t.Fatalf("logout retained session: %v", err)
	}
	requireStatus(t, restored.request("GET", "/chirps/form", nil), 303)
	csrf = csrfFrom(t, b.request("GET", "/login", nil))
	requireStatus(t, b.request("POST", "/login", url.Values{"csrf_token": {csrf}, "email": {"birder@example.com"}, "password": {"wrong"}}), 401)
	result = b.request("POST", "/login", url.Values{"csrf_token": {csrf}, "email": {"BIRDER@example.com"}, "password": {"birdwatching-passphrase"}, "next": {"https://example.org/phish"}})
	requireStatus(t, result, 303)
	if result.Header().Get("Location") != "/feed" {
		t.Fatal("login allowed external redirect")
	}
	if b.cookies[sessionCookie].Value == cookie.Value {
		t.Fatal("login reused session")
	}
}

func TestRegistrationValidationAndAtomicDuplicates(t *testing.T) {
	store := testStore(t)
	owner := newTestBrowser(t, store)
	csrf := csrfFrom(t, owner.request("GET", "/login?tab=register", nil))
	requireStatus(t, owner.request("POST", "/register", registration(csrf, "one@example.com", "one_birder")), 303)
	for _, tc := range []struct {
		name, email, username, password, confirm string
		status                                   int
	}{
		{"duplicate email", "ONE@example.com", "two_birder", "birdwatching-passphrase", "birdwatching-passphrase", 409},
		{"duplicate username", "two@example.com", "ONE_BIRDER", "birdwatching-passphrase", "birdwatching-passphrase", 409},
		{"invalid email", "bad-address", "two_birder", "birdwatching-passphrase", "birdwatching-passphrase", 422},
		{"display address", "Bird <bird@example.com>", "two_birder", "birdwatching-passphrase", "birdwatching-passphrase", 422},
		{"invalid username", "two@example.com", "bad name", "birdwatching-passphrase", "birdwatching-passphrase", 422},
		{"short password", "two@example.com", "two_birder", "short", "short", 422},
		{"overlong password", "two@example.com", "two_birder", strings.Repeat("a", 73), strings.Repeat("a", 73), 422},
		{"mismatch", "two@example.com", "two_birder", "birdwatching-passphrase", "different", 422},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newTestBrowser(t, store)
			csrf := csrfFrom(t, b.request("GET", "/login?tab=register", nil))
			values := registration(csrf, tc.email, tc.username)
			values.Set("password", tc.password)
			values.Set("confirm_password", tc.confirm)
			rec := b.request("POST", "/register", values)
			requireStatus(t, rec, tc.status)
			html := rec.Body.String()
			if !strings.Contains(html, `action="/register"`) || !strings.Contains(html, `value="/chirps/form?draft=1"`) {
				t.Fatal("error lost tab or next destination")
			}
			if strings.Contains(html, `value="`+tc.password+`"`) {
				t.Fatal("error reflected password")
			}
			if b.cookies[sessionCookie] != nil {
				t.Fatal("invalid registration signed in")
			}
		})
	}
	var users, members int
	store.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&users)
	store.db.QueryRow("SELECT COUNT(*) FROM members").Scan(&members)
	if users != 1 || members != 1 {
		t.Fatalf("failed registration left records: users=%d members=%d", users, members)
	}
}

func TestCSRFAndExpiredSessions(t *testing.T) {
	store := testStore(t)
	for _, path := range []string{"/login", "/register", "/logout"} {
		for _, token := range []string{"", "forged"} {
			b := newTestBrowser(t, store)
			requireStatus(t, b.request("POST", path, url.Values{"csrf_token": {token}}), 403)
		}
	}
	a := newTestBrowser(t, store)
	b := newTestBrowser(t, store)
	csrfA := csrfFrom(t, a.request("GET", "/login", nil))
	csrfB := csrfFrom(t, b.request("GET", "/login", nil))
	if csrfA == csrfB {
		t.Fatal("guest CSRF tokens are not unique")
	}
	requireStatus(t, b.request("POST", "/register", registration(csrfA, "b@example.com", "bird_b")), 403)
	b.cookies[csrfCookie] = &http.Cookie{Name: csrfCookie, Value: csrfA[:len(csrfA)-1] + "x"}
	requireStatus(t, b.request("POST", "/register", registration(b.cookies[csrfCookie].Value, "b@example.com", "bird_b")), 403)
	requireStatus(t, a.request("POST", "/register", registration(csrfA, "a@example.com", "bird_a")), 303)
	token := a.cookies[sessionCookie].Value
	if _, err := store.db.Exec("UPDATE sessions SET expires_at = ?", time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, a.request("GET", "/chirps/form", nil), 303)
	if a.cookies[sessionCookie] != nil {
		t.Fatal("expired cookie not cleared")
	}
	requireStatus(t, a.request("POST", "/logout", url.Values{"csrf_token": {csrfA}}), 403)
	if _, err := store.Session(context.Background(), token); err != sql.ErrNoRows {
		t.Fatalf("expired session accepted: %v", err)
	}
}

func TestSharedNavigationAndPrivacy(t *testing.T) {
	store := testStore(t)
	b := newTestBrowser(t, store)
	for _, path := range []string{"/", "/login", "/login?tab=register", "/feed", "/species", "/flocks", "/flocks/1", "/search"} {
		rec := b.request("GET", path, nil)
		requireStatus(t, rec, 200)
		for _, target := range []string{"/feed", "/species", "/flocks", "/search", "/me", "/chirps/form", "/flocks/new"} {
			if !strings.Contains(rec.Body.String(), `href="`+target+`"`) {
				t.Fatalf("%s missing navigation to %s", path, target)
			}
		}
	}
	for _, path := range []string{"/me", "/chirps/form?draft=1", "/flocks/new"} {
		rec := b.request("GET", path, nil)
		requireStatus(t, rec, 303)
		u, err := url.Parse(rec.Header().Get("Location"))
		if err != nil {
			t.Fatal(err)
		}
		if u.Query().Get("next") != path {
			t.Fatalf("lost return path for %s", path)
		}
	}
	for _, path := range []string{"/members/0", "/members/bad", "/members/999", "/unknown"} {
		requireStatus(t, b.request("GET", path, nil), 404)
	}
	if err := store.Register(context.Background(), "writer@example.com", "writer", "birdwatching-passphrase"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := store.db.Exec(`INSERT INTO flocks(id,name,region,topic,description,created_by,created_at) VALUES (1,'test','','','',1,?)`, now); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 5; i++ {
		if _, err := store.db.Exec(`INSERT INTO chirps(member_id,content,visibility,created_at,updated_at) VALUES (1,?,'public',?,?)`, fmt.Sprintf("public-%d <script>alert(1)</script>", i), now, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.db.Exec(`INSERT INTO chirps(member_id,flock_id,content,visibility,created_at,updated_at) VALUES (1,1,'private-secret','flock',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	rec := b.request("GET", "/", nil)
	requireStatus(t, rec, 200)
	html := rec.Body.String()
	if strings.Contains(html, "private-secret") || strings.Contains(html, "writer@example.com") || strings.Contains(html, "<script>alert") {
		t.Fatal("home leaked private content or unescaped HTML")
	}
	if strings.Contains(html, "public-1") || strings.Contains(html, "public-2") || !strings.Contains(html, "public-5") {
		t.Fatal("home did not limit previews to newest three public posts")
	}
}

func TestSafeReturnDestinations(t *testing.T) {
	for _, raw := range []string{"", "https://evil.example", "//evil.example", "/\\evil.example", "/%2fevil.example", "/%5cevil.example", "/login?tab=register", "/logout", "/\r\nLocation: evil"} {
		if got := safeNext(raw); got != "/feed" {
			t.Errorf("safeNext(%q) = %q", raw, got)
		}
	}
	if got := safeNext("/chirps/form?draft=1"); got != "/chirps/form?draft=1" {
		t.Fatal(got)
	}
}

func TestSecureCookies(t *testing.T) {
	t.Setenv("COOKIE_SECURE", "true")
	b := newTestBrowser(t, testStore(t))
	csrf := csrfFrom(t, b.request("GET", "/login", nil))
	if !b.cookies[csrfCookie].Secure {
		t.Fatal("guest cookie missing Secure")
	}
	requireStatus(t, b.request("POST", "/register", registration(csrf, "secure@example.com", "secure_bird")), 303)
	if !b.cookies[sessionCookie].Secure {
		t.Fatal("session cookie missing Secure")
	}
}
