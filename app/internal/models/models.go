package models

import "io"

// UploadRequest is the request body for the file upload endpoint.
type UploadRequest struct {
	AssetType string `json:"asset_type"`
	AssetID   string `json:"asset_id"`
}

// DownloadResult holds the streaming body and metadata for a downloaded object.
type DownloadResult struct {
	Body        io.ReadCloser `json:"-"`
	ContentType string        `json:"content_type"`
	Size        int64         `json:"size"`
	FileName    string        `json:"file_name"`
}

// UploadResponse is returned after a successful file upload.
type UploadResponse struct {
	Bucket string `json:"bucket"`
	Key    string `json:"key"`
}

// S3UploadResponse mirrors the response from the MinIO service's POST /upload.
type S3UploadResponse struct {
	Bucket string `json:"bucket"`
	Key    string `json:"key"`
}
