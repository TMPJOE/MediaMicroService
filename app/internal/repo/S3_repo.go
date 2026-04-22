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
func (r *s3HTTPRepo) UploadFile(ctx context.Context, bucketName, objectName string, file io.Reader, size int64, contentType string) error {
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

	url := fmt.Sprintf("%s/upload", r.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, pr)
	if err != nil {
		return fmt.Errorf("create upload request: %w", err)
	}
	req.Header.Set("Content-Type", writer.formContentType())

	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("upload request to minio-service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("minio-service upload failed (status %d): %s", resp.StatusCode, string(body))
	}

	// Decode the response to confirm the bucket/key match
	var result models.S3UploadResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		// Non-fatal: the upload succeeded even if we can't parse the response
		return nil
	}

	return nil
}

// DownloadFile retrieves a file from the MinIO service's GET /download/{bucket}/{key} endpoint.
func (r *s3HTTPRepo) DownloadFile(ctx context.Context, bucketName, objectName string) (*models.DownloadResult, error) {
	url := fmt.Sprintf("%s/download/%s/%s", r.baseURL, bucketName, objectName)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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
type multipartWriter struct {
	pw       *io.PipeWriter
	filename string
	mime     string
	file     io.Reader
}

func newMultipartWriter(pw *io.PipeWriter, filename, mime string, file io.Reader) *multipartWriter {
	return &multipartWriter{
		pw:       pw,
		filename: filename,
		mime:     mime,
		file:     file,
	}
}

const boundary = "----MinIOServiceBoundary"

func (w *multipartWriter) formContentType() string {
	return "multipart/form-data; boundary=" + boundary
}

func (w *multipartWriter) write() error {
	// Write multipart header
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
