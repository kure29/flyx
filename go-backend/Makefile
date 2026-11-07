GO ?= go
PNPM ?= pnpm
THEME_DIST := internal/statustheme/builtin/dist

.PHONY: test build run status-theme

test:
	$(GO) test ./...

build:
	$(GO) build ./cmd/paneld

run:
	SERVER_ADDR=:6365 $(GO) run ./cmd/paneld

# Builds the built-in status page theme from vite-frontend and puts it where
# the binary embeds it (ignored by git, like every dist/). Without it, /status/ answers that it is not built.
status-theme:
	cd ../vite-frontend && $(PNPM) install --frozen-lockfile && $(PNPM) run build:status-theme
	rm -rf $(THEME_DIST) && mkdir -p $(THEME_DIST)
	cp -R ../vite-frontend/dist-status-theme/. $(THEME_DIST)/
