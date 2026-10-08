# The Templ executable is installed in Go's binary directory by default.
TEMPL ?= $(shell go env GOPATH)/bin/templ
# The standalone Tailwind executable is kept in the ignored .tools directory.
TAILWIND ?= ./.tools/tailwindcss

.PHONY: generate css build test run

# Regenerate Go code whenever a .templ source file changes.
generate:
	$(TEMPL) generate

# Compile the Tailwind input into the stylesheet served by Echo.
css:
	$(TAILWIND) -i ./static/css/input.css -o ./static/css/output.css --minify

# Prepare generated assets for running or testing the application.
build: generate css

# Regenerate templates before compiling and running all Go tests.
test: generate
	go test ./...

# Prepare assets and start the Echo development server.
run: build
	go run .
