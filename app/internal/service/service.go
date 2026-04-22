// Package service contains the business logic layer of the Media service.
// It defines service interfaces and implements use cases by orchestrating
// the database repository and the S3 HTTP client (MinIO service), applying
// business rules, and returning results to handlers.
package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"

	"hotel.com/app/internal/models"
	"hotel.com/app/internal/repo"
)

// Service defines all business operations exposed to the handler layer.
type Service interface {
	Check() error
	UploadFile(ctx context.Context, bucket, filename string, file io.Reader, size int64, contentType string) (*models.UploadResponse, error)
	DownloadFile(ctx context.Context, bucket, key string) (*models.DownloadResult, error)
}

type mediaService struct {
	l  *slog.Logger
	db repo.ServiceRepository
	s3 repo.S3Repository
}

// New constructs a Service wired to both a database repo and an S3 HTTP client.
func New(l *slog.Logger, db repo.ServiceRepository, s3 repo.S3Repository) Service {
	return &mediaService{
		l:  l,
		db: db,
		s3: s3,
	}
}

// Check pings the database to verify connectivity (used by the readiness probe).
func (s *mediaService) Check() error {
	s.l.Info("Pinging db...")
	err := s.db.DbPing()
	if err != nil {
		s.l.Error("db ping failed", "err", err.Error())
		return err
	}
	s.l.Info("db ping ok")
	return nil
}

// UploadFile validates the input, generates the object key, and forwards
// the file to the MinIO service for storage.
// The object key is sanitised to prevent path traversal.
func (s *mediaService) UploadFile(ctx context.Context, bucket, filename string, file io.Reader, size int64, contentType string) (*models.UploadResponse, error) {
	if strings.TrimSpace(bucket) == "" {
		return nil, fmt.Errorf("bucket name must not be empty")
	}
	if strings.TrimSpace(filename) == "" {
		return nil, fmt.Errorf("filename must not be empty")
	}

	// Sanitise: keep only the base name so path traversal is impossible.
	key := filepath.Base(filepath.Clean(filename))

	if err := s.s3.UploadFile(ctx, bucket, key, file, size, contentType); err != nil {
		s.l.Error("upload failed", "bucket", bucket, "key", key, "err", err)
		return nil, err
	}

	s.l.Info("file uploaded", "bucket", bucket, "key", key)
	return &models.UploadResponse{Bucket: bucket, Key: key}, nil
}

// DownloadFile fetches the object from the MinIO service and returns the streaming result.
func (s *mediaService) DownloadFile(ctx context.Context, bucket, key string) (*models.DownloadResult, error) {
	if strings.TrimSpace(bucket) == "" {
		return nil, fmt.Errorf("bucket name must not be empty")
	}
	if strings.TrimSpace(key) == "" {
		return nil, fmt.Errorf("object key must not be empty")
	}

	result, err := s.s3.DownloadFile(ctx, bucket, key)
	if err != nil {
		s.l.Error("download failed", "bucket", bucket, "key", key, "err", err)
		return nil, err
	}

	s.l.Info("file downloaded", "bucket", bucket, "key", key, "size", result.Size)
	return result, nil
}
