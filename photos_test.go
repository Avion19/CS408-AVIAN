package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type testUpload struct {
	field, name string
	data        []byte
}

func multipartChirp(t *testing.T, browser *testBrowser, values url.Values, files ...testUpload) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for field, entries := range values {
		for _, entry := range entries {
			if err := writer.WriteField(field, entry); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, file := range files {
		field := file.field
		if field == "" {
			field = "photo"
		}
		part, err := writer.CreateFormFile(field, file.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(file.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/chirps/form", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	for _, cookie := range browser.cookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	browser.server.ServeHTTP(rec, req)
	return rec
}

func testPhoto(t *testing.T, format string, width, height int) []byte {
	t.Helper()
	pixels := image.NewRGBA(image.Rect(0, 0, width, height))
	pixels.Set(0, 0, color.RGBA{R: 200, G: 100, B: 10, A: 255})
	var out bytes.Buffer
	var err error
	if format == "jpeg" {
		err = jpeg.Encode(&out, pixels, nil)
	} else {
		err = png.Encode(&out, pixels)
	}
	if err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func uploadCount(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

func TestPhotoValidationAndMultipartCSRF(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UPLOAD_DIR", dir)
	store := testStore(t)
	owner, _ := profileAccount(t, store, "upload_birder")
	csrf := csrfFrom(t, owner.request("GET", "/chirps/form", nil))
	valid := testPhoto(t, "jpeg", 8, 6)
	cases := []struct {
		name       string
		data       []byte
		permission string
		status     int
	}{
		{"invalid image", []byte("<svg onload='alert(1)'></svg>"), "yes", 422},
		{"GIF", []byte("GIF89a"), "yes", 422},
		{"truncated JPEG", valid[:30], "yes", 422},
		{"dimension limit", testPhoto(t, "png", 3001, 1), "yes", 422},
		{"height limit", testPhoto(t, "png", 1, 3001), "yes", 422},
		{"over 5 MB", append(append([]byte{}, valid...), make([]byte, maxPhotoBytes)...), "yes", 422},
		{"request limit", make([]byte, maxChirpRequestBytes+1), "yes", 413},
		{"unconfirmed rights", valid, "", 422},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			values := chirpValues(csrf, "Retain my photo observation.")
			values.Set("photo_permission", tc.permission)
			page := multipartChirp(t, owner, values, testUpload{name: "bird.jpg", data: tc.data})
			requireStatus(t, page, tc.status)
			if tc.status == 422 && (!strings.Contains(page.Body.String(), "Retain my photo observation.") || !strings.Contains(page.Body.String(), `value="1" checked`)) {
				t.Fatal("invalid upload lost content or tags")
			}
			if uploadCount(t, dir) != 0 {
				t.Fatal("invalid upload left files")
			}
		})
	}
	values := chirpValues(csrf, "Multiple uploads")
	values.Set("photo_permission", "yes")
	requireStatus(t, multipartChirp(t, owner, values, testUpload{name: "one.jpg", data: valid}, testUpload{name: "two.jpg", data: valid}), 422)
	requireStatus(t, multipartChirp(t, owner, values, testUpload{field: "unexpected", name: "one.jpg", data: valid}), 422)
	values.Set("csrf_token", "forged")
	requireStatus(t, multipartChirp(t, owner, values, testUpload{name: "bird.jpg", data: valid}), 403)
	if uploadCount(t, dir) != 0 {
		t.Fatal("CSRF rejection wrote photo")
	}
	values.Set("csrf_token", csrf)
	// Browsers send an empty file part when no file is selected.
	requireStatus(t, multipartChirp(t, owner, values, testUpload{name: "", data: nil}), 303)
	if uploadCount(t, dir) != 0 || lastChirp(t, store).ImagePath != "" {
		t.Fatal("empty file control created a photo")
	}
}

func TestPhotoReencodingReplacementAndCleanup(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UPLOAD_DIR", dir)
	store := testStore(t)
	owner, _ := profileAccount(t, store, "photo_author")
	csrf := csrfFrom(t, owner.request("GET", "/chirps/form", nil))
	original := testPhoto(t, "jpeg", 8, 6)
	// Insert a legal JPEG APP1 segment containing a recognizable EXIF marker.
	marker := []byte("Exif\x00\x00GPS-PRIVATE-LOCATION")
	segment := make([]byte, len(marker)+4)
	segment[0], segment[1] = 0xff, 0xe1
	binary.BigEndian.PutUint16(segment[2:4], uint16(len(marker)+2))
	copy(segment[4:], marker)
	source := append(append(append([]byte{}, original[:2]...), segment...), original[2:]...)
	values := chirpValues(csrf, "Photo observation")
	values.Set("photo_permission", "yes")
	requireStatus(t, multipartChirp(t, owner, values, testUpload{name: "../../untrusted.html", data: source}), 303)
	chirp := lastChirp(t, store)
	if !safePhotoName(chirp.ImagePath) || filepath.Ext(chirp.ImagePath) != ".jpg" || chirp.ImagePath == "untrusted.html" {
		t.Fatalf("unsafe filename: %s", chirp.ImagePath)
	}
	saved, err := os.ReadFile(filepath.Join(dir, chirp.ImagePath))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(saved, marker) {
		t.Fatal("EXIF metadata survived re-encoding")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(saved))
	if err != nil || format != "jpeg" || config.Width != 8 || config.Height != 6 {
		t.Fatal("saved photo is not a valid re-encoded JPEG")
	}
	info, err := os.Stat(filepath.Join(dir, chirp.ImagePath))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("uploaded photo permissions are not private")
	}
	guest := newTestBrowser(t, store)
	photoURL := fmt.Sprintf("/chirps/%d/photo", chirp.ID)
	response := guest.request("GET", photoURL, nil)
	requireStatus(t, response, 200)
	if response.Header().Get("Content-Type") != "image/jpeg" || response.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(response.Header().Get("Cache-Control"), "no-store") {
		t.Fatal("photo headers missing")
	}
	// Uploaded bytes never have a public static URL.
	requireStatus(t, guest.request("GET", "/static/uploads/"+chirp.ImagePath, nil), 404)
	values.Set("id", fmt.Sprint(chirp.ID))
	// Failed edit must retain the old photo and clean up its new upload.
	values.Set("species_id", "99999")
	requireStatus(t, multipartChirp(t, owner, values, testUpload{name: "replace.png", data: testPhoto(t, "png", 3, 4)}), 422)
	if uploadCount(t, dir) != 1 {
		t.Fatal("failed edit left replacement file")
	}
	if current := lastChirp(t, store); current.ImagePath != chirp.ImagePath {
		t.Fatal("failed edit replaced original photo")
	}
	// A database failure after writing a valid image also cleans up the new file.
	values.Set("species_id", "2")
	execFixture(t, store, `CREATE TRIGGER fail_photo_tag BEFORE INSERT ON chirp_species BEGIN SELECT RAISE(ABORT, 'fixture photo failure'); END`)
	requireStatus(t, multipartChirp(t, owner, values, testUpload{name: "replace.png", data: testPhoto(t, "png", 3, 4)}), 500)
	if uploadCount(t, dir) != 1 {
		t.Fatal("database rollback left a new photo")
	}
	execFixture(t, store, `DROP TRIGGER fail_photo_tag`)
	pngPhoto := append(testPhoto(t, "png", 3, 4), []byte("PRIVATE-TRAILING-METADATA")...)
	requireStatus(t, multipartChirp(t, owner, values, testUpload{name: "replace.jpg", data: pngPhoto}), 303)
	replacement := lastChirp(t, store)
	if replacement.ImagePath == chirp.ImagePath || filepath.Ext(replacement.ImagePath) != ".png" || uploadCount(t, dir) != 1 {
		t.Fatal("replacement did not remove previous image")
	}
	if _, err := os.Stat(filepath.Join(dir, chirp.ImagePath)); !os.IsNotExist(err) {
		t.Fatal("old image was not removed")
	}
	response = guest.request("GET", photoURL, nil)
	requireStatus(t, response, 200)
	if response.Header().Get("Content-Type") != "image/png" || bytes.Contains(response.Body.Bytes(), []byte("PRIVATE-TRAILING-METADATA")) {
		t.Fatal("PNG was not re-encoded")
	}
	values.Set("remove_photo", "yes")
	requireStatus(t, owner.request("POST", "/chirps/form", values), 303)
	if uploadCount(t, dir) != 0 || lastChirp(t, store).ImagePath != "" {
		t.Fatal("photo removal did not clear file and database")
	}
	requireStatus(t, guest.request("GET", photoURL, nil), 404)
	values.Del("remove_photo")
	requireStatus(t, multipartChirp(t, owner, values, testUpload{name: "again.jpg", data: original}), 303)
	requireStatus(t, owner.request("POST", fmt.Sprintf("/chirps/%d/delete", chirp.ID), url.Values{"csrf_token": {csrf}, "confirm_delete": {"yes"}}), 303)
	if uploadCount(t, dir) != 0 {
		t.Fatal("chirp deletion left photo")
	}
	requireStatus(t, guest.request("GET", photoURL, nil), 404)
}

func TestPhotoVisibilityMembershipChangesAndMissingFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UPLOAD_DIR", dir)
	store := testStore(t)
	owner, member := profileAccount(t, store, "private_author")
	eligible, eligibleMember := profileAccount(t, store, "private_viewer")
	outsider, _ := profileAccount(t, store, "private_outsider")
	guest := newTestBrowser(t, store)
	flock := fixtureFlock(t, store, member, "Private photo flock")
	execFixture(t, store, `INSERT INTO flock_members(flock_id, member_id, role, joined_at) VALUES (?, ?, 'member', ?)`, flock, eligibleMember.ID, time.Now().UTC().Format(time.RFC3339))
	csrf := csrfFrom(t, owner.request("GET", "/chirps/form", nil))
	values := chirpValues(csrf, "Membership protected photo")
	values.Set("photo_permission", "yes")
	values.Set("visibility", "flock")
	values.Set("flock_id", fmt.Sprint(flock))
	requireStatus(t, multipartChirp(t, owner, values, testUpload{name: "private.jpg", data: testPhoto(t, "jpeg", 4, 4)}), 303)
	chirp := lastChirp(t, store)
	photoURL := fmt.Sprintf("/chirps/%d/photo", chirp.ID)
	requireStatus(t, owner.request("GET", photoURL, nil), 200)
	requireStatus(t, eligible.request("GET", photoURL, nil), 200)
	requireStatus(t, outsider.request("GET", photoURL, nil), 404)
	requireStatus(t, guest.request("GET", photoURL, nil), 404)
	execFixture(t, store, `DELETE FROM flock_members WHERE flock_id = ? AND member_id = ?`, flock, eligibleMember.ID)
	requireStatus(t, eligible.request("GET", photoURL, nil), 404)
	values.Set("id", fmt.Sprint(chirp.ID))
	values.Set("visibility", "public")
	requireStatus(t, owner.request("POST", "/chirps/form", values), 303)
	requireStatus(t, guest.request("GET", photoURL, nil), 200)
	values.Set("visibility", "flock")
	requireStatus(t, owner.request("POST", "/chirps/form", values), 303)
	requireStatus(t, guest.request("GET", photoURL, nil), 404)
	if err := os.Remove(filepath.Join(dir, chirp.ImagePath)); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, owner.request("GET", photoURL, nil), 404)
	// Neither unsafe legacy paths nor symlinks can expose other files.
	for _, path := range []string{"../outside.jpg", "/tmp/outside.jpg", `..\outside.jpg`, "bad.svg"} {
		execFixture(t, store, `UPDATE chirps SET image_path = ? WHERE id = ?`, path, chirp.ID)
		requireStatus(t, owner.request("GET", photoURL, nil), 404)
	}
	outside := filepath.Join(t.TempDir(), "outside.jpg")
	if err := os.WriteFile(outside, []byte("private file"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link.jpg")); err != nil {
		t.Fatal(err)
	}
	execFixture(t, store, `UPDATE chirps SET image_path = 'link.jpg' WHERE id = ?`, chirp.ID)
	requireStatus(t, owner.request("GET", photoURL, nil), 404)
}
