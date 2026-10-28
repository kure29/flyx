package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	// The runtime image has no zoneinfo; embed it so TZ (e.g. Asia/Shanghai
	// in the compose files) applies to logs and notification times.
	_ "time/tzdata"

	"go-backend/internal/app"
	"go-backend/internal/config"
)

func main() {
	cfg := config.FromEnv()
	if len(os.Args) > 1 && os.Args[1] == "enable-password-login" {
		enablePasswordLogin(cfg)
		return
	}
	generated, err := config.EnsureJWTSecret(&cfg)
	if err != nil {
		log.Fatalf("JWT_SECRET is empty and no secret could be stored: %v", err)
	}
	if generated {
		log.Printf("JWT_SECRET is empty; generated one and saved it next to the database")
	}
	log.Printf("starting go-backend on %s (db=%s)", cfg.Addr, cfg.DBPath)

	a, err := app.New(cfg)
	if err != nil {
		log.Fatalf("failed to create app: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- a.Run()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Printf("received signal %s, shutting down", sig)
	case runErr := <-errCh:
		if runErr != nil && !errors.Is(runErr, http.ErrServerClosed) {
			log.Fatalf("server stopped unexpectedly: %v", runErr)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := a.Shutdown(ctx); err != nil {
		log.Fatalf("shutdown failed: %v", err)
	}
}

// enablePasswordLogin lets admins log in with their password again, for an
// admin locked out after turning it off (e.g. GitHub unreachable):
// docker compose exec backend /app/paneld enable-password-login
func enablePasswordLogin(cfg config.Config) {
	r, err := app.OpenRepository(cfg)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer r.Close()
	if err := r.UpsertConfig("admin_password_login_disabled", "false", time.Now().UnixMilli()); err != nil {
		log.Fatalf("enable password login: %v", err)
	}
	fmt.Println("已允许管理员使用密码登录")
}
