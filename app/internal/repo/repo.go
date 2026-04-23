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
	// SaveHotelImage persists a reference to an uploaded hotel image and returns the new row id.
	SaveHotelImage(ctx context.Context, hotelID, bucket, objectKey, contentType string, fileSize int64) (int64, error)
	// GetHotelImages returns a list of object keys for a given hotel, ordered by newest first.
	GetHotelImages(ctx context.Context, hotelID string) ([]models.S3UploadResponse, error)

	// SaveRoomImage persists a reference to an uploaded room image and returns the new row id.
	SaveRoomImage(ctx context.Context, roomID, bucket, objectKey, contentType string, fileSize int64) (int64, error)
	// GetRoomImages returns a list of object keys for a given room, ordered by newest first.
	GetRoomImages(ctx context.Context, roomID string) ([]models.S3UploadResponse, error)
}

// S3Repository defines the object-storage operations delegated to the MinIO service.
type S3Repository interface {
	// UploadFile forwards a file upload to the MinIO service.
	UploadFile(ctx context.Context, bucketName, objectName string, file io.Reader, size int64, contentType string) (string, error)

	// DownloadFile retrieves a file from the MinIO service by bucket and object key.
	DownloadFile(ctx context.Context, bucketName, objectName string) (*models.DownloadResult, error)

	// BuildDownloadURL constructs a full HTTP URL for downloading the given bucket/key
	// via the MinIO service (e.g. "http://minio-service:8080/download/{bucket}/{key}").
	BuildDownloadURL(bucketName, objectName string) string
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

// SaveHotelImage inserts a hotel image record and returns the inserted id.
func (r *databaseRepo) SaveHotelImage(ctx context.Context, hotelID, bucket, objectKey, contentType string, fileSize int64) (int64, error) {
	var id int64
	row := r.pool.QueryRow(ctx, `INSERT INTO hotel_images (hotel_id, bucket, object_key, content_type, file_size) VALUES ($1,$2,$3,$4,$5) RETURNING id`, hotelID, bucket, objectKey, contentType, fileSize)
	if err := row.Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

// GetHotelImages returns S3 upload responses (bucket/key) for the given hotel_id.
func (r *databaseRepo) GetHotelImages(ctx context.Context, hotelID string) ([]models.S3UploadResponse, error) {
	rows, err := r.pool.Query(ctx, `SELECT bucket, object_key FROM hotel_images WHERE hotel_id = $1 ORDER BY created_at DESC`, hotelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.S3UploadResponse
	for rows.Next() {
		var b, k string
		if err := rows.Scan(&b, &k); err != nil {
			return nil, err
		}
		out = append(out, models.S3UploadResponse{Bucket: b, Key: k})
	}
	return out, nil
}

// SaveRoomImage inserts a room image record and returns the inserted id.
func (r *databaseRepo) SaveRoomImage(ctx context.Context, roomID, bucket, objectKey, contentType string, fileSize int64) (int64, error) {
	var id int64
	row := r.pool.QueryRow(ctx, `INSERT INTO room_images (room_id, bucket, object_key, content_type, file_size) VALUES ($1,$2,$3,$4,$5) RETURNING id`, roomID, bucket, objectKey, contentType, fileSize)
	if err := row.Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

// GetRoomImages returns S3 upload responses (bucket/key) for the given room_id.
func (r *databaseRepo) GetRoomImages(ctx context.Context, roomID string) ([]models.S3UploadResponse, error) {
	rows, err := r.pool.Query(ctx, `SELECT bucket, object_key FROM room_images WHERE room_id = $1 ORDER BY created_at DESC`, roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.S3UploadResponse
	for rows.Next() {
		var b, k string
		if err := rows.Scan(&b, &k); err != nil {
			return nil, err
		}
		out = append(out, models.S3UploadResponse{Bucket: b, Key: k})
	}
	return out, nil
}
