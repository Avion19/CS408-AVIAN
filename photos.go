package main

import (
	"bytes"
	"database/sql"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v4"
)

const maxPhotoBytes = 5 << 20
const maxChirpRequestBytes = 6 << 20

type photoInputError struct{ message string }

func (e *photoInputError) Error() string { return e.message }

func uploadDirectory() string {
	if path := os.Getenv("UPLOAD_DIR"); path != "" {
		return path
	}
	return "./data/uploads"
}

func safePhotoName(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name && !strings.ContainsAny(name, "/\\")
}

func removePhoto(c echo.Context, name string) {
	if name == "" {
		return
	}
	if !safePhotoName(name) {
		c.Logger().Error("Skipped unsafe stored photo filename")
		return
	}
	if err := os.Remove(filepath.Join(uploadDirectory(), name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		c.Logger().Errorf("Photo cleanup failed: %v", err)
	}
}

// Only decoded JPEG/PNG pixels reach disk. Re-encoding discards EXIF, including
// location metadata, and ignores the client filename and declared Content-Type.
func receivePhoto(c echo.Context, permission bool) (string, error) {
	form := c.Request().MultipartForm
	if form == nil {
		return "", nil
	}
	var uploads []*multipart.FileHeader
	for field, files := range form.File {
		for _, file := range files {
			if file.Filename == "" {
				continue
			}
			if field != "photo" {
				return "", &photoInputError{"Upload one photo using the photo field."}
			}
			uploads = append(uploads, file)
		}
	}
	if len(uploads) == 0 {
		return "", nil
	}
	if len(uploads) != 1 {
		return "", &photoInputError{"Attach only one photo per chirp."}
	}
	if !permission {
		return "", &photoInputError{"Confirm that you own or have permission to share this photo."}
	}
	if uploads[0].Size > maxPhotoBytes {
		return "", &photoInputError{"Choose a JPEG or PNG no larger than 5 MB."}
	}
	file, err := uploads[0].Open()
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxPhotoBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxPhotoBytes {
		return "", &photoInputError{"Choose a JPEG or PNG no larger than 5 MB."}
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png") {
		return "", &photoInputError{"Choose a valid JPEG or PNG photo."}
	}
	if config.Width < 1 || config.Height < 1 || config.Width > 3000 || config.Height > 3000 {
		return "", &photoInputError{"Keep photo dimensions within 3000 by 3000 pixels."}
	}
	pixels, decodedFormat, err := image.Decode(bytes.NewReader(data))
	if err != nil || decodedFormat != format {
		return "", &photoInputError{"This photo could not be decoded. Choose another JPEG or PNG."}
	}
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	ext := ".jpg"
	if format == "png" {
		ext = ".png"
	}
	name := token + ext
	if err := os.MkdirAll(uploadDirectory(), 0700); err != nil {
		return "", err
	}
	output, err := os.OpenFile(filepath.Join(uploadDirectory(), name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	if format == "png" {
		err = png.Encode(output, pixels)
	} else {
		err = jpeg.Encode(output, pixels, &jpeg.Options{Quality: 88})
	}
	closeErr := output.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		removePhoto(c, name)
		return "", err
	}
	return name, nil
}

func (a *application) chirpPhoto(c echo.Context) error {
	id, err := chirpID(c.Param("id"))
	if err != nil {
		return err
	}
	chirp, err := a.store.VisibleChirp(c.Request().Context(), id, viewerID(c))
	if errors.Is(err, sql.ErrNoRows) {
		return echo.NewHTTPError(http.StatusNotFound, "This photo could not be found.")
	}
	if err != nil {
		return err
	}
	if !safePhotoName(chirp.ImagePath) {
		return echo.NewHTTPError(http.StatusNotFound, "This photo could not be found.")
	}
	contentType := ""
	switch filepath.Ext(chirp.ImagePath) {
	case ".jpg":
		contentType = "image/jpeg"
	case ".png":
		contentType = "image/png"
	default:
		return echo.NewHTTPError(http.StatusNotFound, "This photo could not be found.")
	}
	path := filepath.Join(uploadDirectory(), chirp.ImagePath)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) || err == nil && !info.Mode().IsRegular() {
		return echo.NewHTTPError(http.StatusNotFound, "This photo could not be found.")
	}
	if err != nil {
		return err
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return echo.NewHTTPError(http.StatusNotFound, "This photo could not be found.")
	}
	if err != nil {
		return err
	}
	defer file.Close()
	c.Response().Header().Set("Cache-Control", "private, no-store")
	c.Response().Header().Set("Content-Disposition", "inline")
	return c.Stream(http.StatusOK, contentType, file)
}
