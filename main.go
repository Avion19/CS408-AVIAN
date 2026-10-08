// Package main starts the GoTH Hello World web application.
package main

import (
	"net/http"
	"os"

	"github.com/a-h/templ"
	"github.com/labstack/echo/v4"

	"goth-hello-world/views"
)

// render writes the HTTP status code and renders a Templ component into the
// current Echo response.
func render(c echo.Context, statusCode int, component templ.Component) error {
	c.Response().WriteHeader(statusCode)

	return component.Render(
		c.Request().Context(),
		c.Response().Writer,
	)
}

// homeHandler serves the page for the application's root route.
func homeHandler(c echo.Context) error {
	return render(c, http.StatusOK, views.Home())
}

// healthHandler reports that the application is ready to receive requests.
func healthHandler(c echo.Context) error {
	return c.NoContent(http.StatusOK)
}

// main configures Echo's static-file and application routes, then starts the
// server on the configured port (8080 by default).
func main() {
	e := echo.New()

	e.Static("/static", "static")
	e.GET("/", homeHandler)
	e.GET("/api/health", healthHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	e.Logger.Fatal(e.Start(":" + port))
}
