package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"hotel.com/app/internal/models"
)

// s3HTTPRepo implements S3Repository by calling the MinIO service's REST API.
type s3HTTPRepo struct {
	client        *http.Client
	baseURL       string
	defaultBucket string
}

// NewS3HTTPRepo creates an S3Repository that communicates with the MinIO service
// via its HTTP API (POST /upload, GET /download/{bucket}/{key}).
func NewS3HTTPRepo(baseURL, defaultBucket string) S3Repository {
	return &s3HTTPRepo{
		client:        &http.Client{},
		baseURL:       strings.TrimRight(baseURL, "/"),
		defaultBucket: defaultBucket,
	}
}

// UploadFile forwards a file upload to the MinIO service's POST /upload endpoint.
func (r *s3HTTPRepo) UploadFile(ctx context.Context, bucketName, objectName string, file io.Reader, size int64, contentType string) (string, error) {
	// Build a pipe so we can stream the file body into the HTTP request
	// while also setting the Content-Type multipart form.
	pr, pw := io.Pipe()
	writer := newMultipartWriter(pw, objectName, contentType, file)

	go func() {
		if err := writer.write(); err != nil {
			pw.CloseWithError(err)
			return
		}
		pw.Close()
	}()

	// NOTE: The object key is sent as a separate "object_key" form field
	// because Go's multipart.Reader strips directory components from the
	// filename in Content-Disposition (it calls filepath.Base internally).
	// Without this separate field the MinIO service would only receive the
	// base filename, losing the "hotels/{id}/" prefix.

	url := fmt.Sprintf("%s/upload", r.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, pr)
	if err != nil {
		return "", fmt.Errorf("create upload request: %w", err)
	}
	req.Header.Set("Content-Type", writer.formContentType())

	resp, err := r.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload request to minio-service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("minio-service upload failed (status %d): %s", resp.StatusCode, string(body))
	}

	// Decode the response to confirm the bucket/key match
	var result models.S3UploadResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		// Non-fatal: the upload succeeded even if we can't parse the response
		return objectName, nil
	}

	return result.Key, nil
}

// DownloadFile retrieves a file from the MinIO service's GET /download/{bucket}/* endpoint.
// The object key is used as-is since the chi router wildcard captures the full path.
func (r *s3HTTPRepo) DownloadFile(ctx context.Context, bucketName, objectName string) (*models.DownloadResult, error) {
	dlURL := fmt.Sprintf("%s/download/%s/%s", r.baseURL, bucketName, objectName)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dlURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create download request: %w", err)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download request to minio-service: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("minio-service download failed (status %d)", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	var size int64 = -1
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		if parsed, err := parseContentLength(cl); err == nil {
			size = parsed
		}
	}

	fileName := objectName
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if idx := strings.Index(cd, `filename="`); idx != -1 {
			start := idx + len(`filename="`)
			end := strings.Index(cd[start:], `"`)
			if end != -1 {
				fileName = cd[start : start+end]
			}
		}
	}

	return &models.DownloadResult{
		Body:        resp.Body,
		ContentType: contentType,
		Size:        size,
		FileName:    fileName,
	}, nil
}

// BuildDownloadURL returns the relative HTTP URL for downloading an object via the Media service.
// The object key is used as-is since it's already a valid path segment.
func (r *s3HTTPRepo) BuildDownloadURL(bucketName, objectName string) string {
	return fmt.Sprintf("/download/%s/%s", bucketName, objectName)
}

func parseContentLength(s string) (int64, error) {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int64(c-'0')
	}
	return n, nil
}

// multipartWriter helps construct a multipart/form-data request body
// to stream a file to the MinIO service's /upload endpoint.
// It includes an "object_key" form field so the full S3 key (with prefix
// path like "hotels/{id}/{ts}-file.jpg") is preserved — Go's multipart
// reader strips directory components from the filename in
// Content-Disposition, so the key must travel in a separate field.
type multipartWriter struct {
	pw        *io.PipeWriter
	objectKey string // full S3 object key sent as a separate form field
	filename  string // base filename used in Content-Disposition
	mime      string
	file      io.Reader
}

func newMultipartWriter(pw *io.PipeWriter, objectKey, mime string, file io.Reader) *multipartWriter {
	return &multipartWriter{
		pw:        pw,
		objectKey: objectKey,
		filename:  objectKey, // kept for backward compatibility in Content-Disposition
		mime:      mime,
		file:      file,
	}
}

const boundary = "----MinIOServiceBoundary"

func (w *multipartWriter) formContentType() string {
	return "multipart/form-data; boundary=" + boundary
}

func (w *multipartWriter) write() error {
	// 1. Write the object_key form field (preserves full prefix path).
	keyField := fmt.Sprintf(
		"--%s\r\nContent-Disposition: form-data; name=\"object_key\"\r\n\r\n%s\r\n",
		boundary, w.objectKey,
	)
	if _, err := io.WriteString(w.pw, keyField); err != nil {
		return err
	}

	// 2. Write the file part.
	header := fmt.Sprintf(
		"--%s\r\nContent-Disposition: form-data; name=\"file\"; filename=\"%s\"\r\nContent-Type: %s\r\n\r\n",
		boundary, w.filename, w.mime,
	)
	if _, err := io.WriteString(w.pw, header); err != nil {
		return err
	}

	// Stream the file body
	if _, err := io.Copy(w.pw, w.file); err != nil {
		return err
	}

	// Write multipart footer
	footer := fmt.Sprintf("\r\n--%s--\r\n", boundary)
	if _, err := io.WriteString(w.pw, footer); err != nil {
		return err
	}

	return nil
}
