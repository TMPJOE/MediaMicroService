package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"hotel.com/app/internal/config"
	"hotel.com/app/internal/database"
	"hotel.com/app/internal/handler"
	"hotel.com/app/internal/logging"
	"hotel.com/app/internal/repo"
	"hotel.com/app/internal/service"
)

const (
	publicKeyPath = "/app/keys/public.pem"
)

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		fmt.Println("failed to load config:", err)
		os.Exit(1)
	}

	// Create logger
	l := logging.New()
	l.Info("Media service initiated")

	// Database connection
	db, err := database.NewConn(os.Getenv("DATABASE_URL"))
	if err != nil {
		l.Error("connection to database failed", "err", err)
		os.Exit(1)
	}
	l.Info("database connection successful")
	defer db.Close()

	if err := database.RunMigrations(os.Getenv("DATABASE_URL"), l); err != nil {
		os.Exit(1)
	}

	// JWT public key file check
	if _, err := os.Stat(publicKeyPath); os.IsNotExist(err) {
		l.Error("JWT public key file not found", "path", publicKeyPath)
		os.Exit(1)
	}

	// S3 HTTP client — talks to the MinIO service via its REST API
	s3 := repo.NewS3HTTPRepo(cfg.MinIOService.URL, cfg.MinIOService.Bucket)
	l.Info("S3 HTTP client created", "url", cfg.MinIOService.URL, "bucket", cfg.MinIOService.Bucket)

	// Database repository
	dbRepo := repo.NewDatabaseRepo(db)

	// Service
	svc := service.New(l, dbRepo, s3)

	// JWT validator — only needs the public key to validate tokens
	// issued by the UsersMicroService.
	jwtConfig := handler.JWTConfig{
		Issuer: cfg.JWT.Issuer,
	}
	jwtValidator := handler.NewJWTValidator(jwtConfig, publicKeyPath)
	h := handler.New(svc, l, jwtValidator, cfg.MinIOService.Bucket)

	// HTTP server
	mux := h.NewServerMux(nil)
	port := cfg.Server.Port
	if port == 0 {
		port = 8080
	}
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	l.Info("server listening", "addr", srv.Addr)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			l.Error("server failed", "err", err)
			os.Exit(1)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	<-quit

	l.Info("shutting down server...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		l.Error("server forced to shutdown", "err", err)
	}
	l.Info("server stopped")
}
