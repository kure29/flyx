package app

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"go-backend/internal/config"
	"go-backend/internal/geoip"
	httpserver "go-backend/internal/http"
	"go-backend/internal/http/handler"
	"go-backend/internal/store/repo"
)

type App struct {
	cfg    config.Config
	server *http.Server
	repo   *repo.Repository
	h      *handler.Handler
}

// OpenRepository opens the database the config points at.
func OpenRepository(cfg config.Config) (*repo.Repository, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.DBType)) {
	case "", "sqlite":
		r, err := repo.Open(cfg.DBPath)
		if err != nil {
			return nil, fmt.Errorf("open sqlite: %w", err)
		}
		return r, nil
	case "postgres", "postgresql":
		r, err := repo.OpenPostgres(cfg.DatabaseURL)
		if err != nil {
			return nil, fmt.Errorf("open postgres: %w", err)
		}
		return r, nil
	default:
		return nil, fmt.Errorf("unsupported DB_TYPE %q", cfg.DBType)
	}
}

func New(cfg config.Config) (*App, error) {
	r, err := OpenRepository(cfg)
	if err != nil {
		return nil, err
	}

	h := handler.New(r, cfg.JWTSecret)
	h.SetStatusThemesDir(cfg.StatusThemesDir)
	h.SetGeoIP(geoIPFromConfig(cfg))
	router := httpserver.NewRouter(h, cfg.JWTSecret)

	s := &http.Server{
		Addr:              cfg.Addr,
		Handler:           router,
		ReadTimeout:       30 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       60 * time.Second,
	}

	return &App{cfg: cfg, server: s, repo: r, h: h}, nil
}

func (a *App) Run() error {
	if a.h != nil {
		a.h.StartBackgroundJobs()
	}
	return a.server.ListenAndServe()
}

func (a *App) Shutdown(ctx context.Context) error {
	if a.h != nil {
		a.h.StopBackgroundJobs()
	}
	shutdownErr := a.server.Shutdown(ctx)
	closeErr := a.repo.Close()
	if shutdownErr != nil {
		return shutdownErr
	}
	return closeErr
}

// geoIPFromConfig picks the country database for node region detection.
func geoIPFromConfig(cfg config.Config) *geoip.DB {
	switch path := strings.TrimSpace(cfg.GeoIPCountryDB); {
	case strings.EqualFold(path, "off"):
		return nil
	case path != "":
		return geoip.New(path, false)
	default:
		return geoip.New(filepath.Join(filepath.Dir(cfg.DBPath), "geoip", geoip.FileName), true)
	}
}
