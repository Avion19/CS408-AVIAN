// Package main starts the Avian HTTP server and initializes its local data.
package main

import (
	"embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v4"
)

// Templates are embedded so the executable carries the HTML used by the app.
//
//go:embed templates/*.html
var templateFiles embed.FS

// newServer wires the Checkpoint 1 routes to an initialized store.
func newServer(store *Store) (*echo.Echo, error) {
	pages, err := template.ParseFS(templateFiles, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	e := echo.New()
	e.HideBanner = true
	e.Static("/static", "static")
	// Query on each request so the page reflects the persisted catalog.
	e.GET("/", func(c echo.Context) error {
		species, err := store.Species(c.Request().Context())
		if err != nil {
			return err
		}
		c.Response().Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
		return pages.ExecuteTemplate(c.Response(), "home.html", species)
	})
	e.GET("/api/health", func(c echo.Context) error {
		// A database check makes this endpoint useful to the deployment script.
		if err := store.Ping(c.Request().Context()); err != nil {
			return err
		}
		return c.NoContent(http.StatusOK)
	})
	return e, nil
}

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
