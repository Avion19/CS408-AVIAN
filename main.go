// Package main starts the Avian HTTP server and initializes its local data.
package main

import (
	"embed"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// Templates are embedded so the executable carries the HTML used by the app.
//
//go:embed templates/*.html
var templateFiles embed.FS

func main() {
	// The local defaults can be replaced by environment variables or .env via start.sh.
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	if strings.Contains(port, ":") {
		log.Fatal("PORT must be a port number")
	}
	databasePath := os.Getenv("DATABASE_PATH")
	if databasePath == "" {
		databasePath = "./data/avian.db"
	}
	uploadDir := os.Getenv("UPLOAD_DIR")
	if uploadDir == "" {
		uploadDir = "./data/uploads"
	}
	if err := os.MkdirAll(filepath.Dir(databasePath), 0700); err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(uploadDir, 0700); err != nil {
		log.Fatal(err)
	}
	store, err := OpenStore(databasePath)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	if err := store.Initialize(); err != nil {
		log.Fatal(err)
	}
	if len(os.Getenv("SESSION_SECRET")) < 32 {
		log.Fatal("SESSION_SECRET must contain at least 32 characters; use ./start.sh to generate local configuration")
	}
	server, err := newServer(store)
	if err != nil {
		log.Fatal(err)
	}
	listenAddr := os.Getenv("LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = ":" + port
	}
	server.Logger.Fatal(server.Start(listenAddr))
}
