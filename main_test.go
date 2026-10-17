package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestFoundation(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "avian.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for i := 0; i < 2; i++ {
		if err := store.Initialize(); err != nil {
			t.Fatal(err)
		}
	}
	var tables int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN
		('users','members','sessions','species','flocks','flock_members','chirps','chirp_species')`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 8 {
		t.Fatalf("got %d core tables, want 8", tables)
	}
	species, err := store.Species(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(species) != len(seedBirds) {
		t.Fatalf("got %d species, want %d", len(species), len(seedBirds))
	}
	server, err := newServer(store)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("home returned %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "American Robin") {
		t.Fatal("home did not show seeded species")
	}
}
