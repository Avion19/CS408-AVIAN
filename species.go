package main

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
)

func (a *application) speciesPage(c echo.Context) error {
	birds, err := a.store.Species(c.Request().Context())
	if err != nil {
		return err
	}
	data := pageData{Title: "Species", Active: "/species", Species: birds, Query: strings.TrimSpace(c.QueryParam("q"))}
	if raw := c.QueryParam("selected"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id < 1 {
			return echo.NewHTTPError(http.StatusNotFound, "This species could not be found.")
		}
		for i := range birds {
			if birds[i].ID == id {
				data.SelectedSpecies = &birds[i]
				break
			}
		}
		if data.SelectedSpecies == nil {
			return echo.NewHTTPError(http.StatusNotFound, "This species could not be found.")
		}
		data.Page, err = pageNumber(c)
		if err != nil {
			return err
		}
		data.Timeline, data.HasMore, err = a.store.VisibleChirps(c.Request().Context(), viewerID(c), id, data.Page)
		if err != nil {
			return err
		}
		values := url.Values{"selected": {strconv.FormatInt(id, 10)}, "q": {data.Query}, "page": {strconv.Itoa(data.Page - 1)}}
		data.PreviousURL = "/species?" + values.Encode()
		values.Set("page", strconv.Itoa(data.Page+1))
		data.NextURL = "/species?" + values.Encode()
	}
	if data.Query != "" {
		data.Species = nil
		query := strings.ToLower(data.Query)
		for _, bird := range birds {
			if strings.Contains(strings.ToLower(bird.CommonName+" "+bird.ScientificName), query) {
				data.Species = append(data.Species, bird)
			}
		}
	}
	return a.render(c, http.StatusOK, "species.html", data)
}
