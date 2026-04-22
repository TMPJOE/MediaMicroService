// Package repo implements the data access layer of the Media service.
// It handles all database queries via PostgreSQL and delegates S3 operations
// to the MinIO service through its HTTP API.
package repo

import (
	"context"
	"io"

	"github.com/jackc/pgx/v5/pgxpool"
	"hotel.com/app/internal/models"
)

// ServiceRepository defines the database operations required by the service layer.
type ServiceRepository interface {
	DbPing() error
	// Add hotel/room image CRUD methods here as the service grows.
}

// S3Repository defines the object-storage operations delegated to the MinIO service.
type S3Repository interface {
	// UploadFile forwards a file upload to the MinIO service.
	UploadFile(ctx context.Context, bucketName, objectName string, file io.Reader, size int64, contentType string) error

	// DownloadFile retrieves a file from the MinIO service by bucket and object key.
	DownloadFile(ctx context.Context, bucketName, objectName string) (*models.DownloadResult, error)
}

// databaseRepo implements ServiceRepository backed by a pgx connection pool.
type databaseRepo struct {
	pool *pgxpool.Pool
}

// NewDatabaseRepo constructs a ServiceRepository from a pgx connection pool.
func NewDatabaseRepo(pool *pgxpool.Pool) ServiceRepository {
	return &databaseRepo{pool: pool}
}

// DbPing verifies the database is reachable.
func (r *databaseRepo) DbPing() error {
	return r.pool.Ping(context.Background())
}
