# Media Microservice

The **Media Service** is the central image-management layer of the HIC Hotel platform. It owns the PostgreSQL database that tracks image metadata and delegates all object storage to a separate **MinIO Service** via its HTTP API.

## Architecture

```
┌──────────────┐     ┌─────────────────┐     ┌─────────────┐
│  Servicio A  │────▶│  Media Service  │────▶│MinIO Service│────▶ MinIO Container
│  (Hotel API) │     │  (this repo)    │     │ (separate)  │
└──────────────┘     │                 │     └─────────────┘
┌──────────────┐     │  PostgreSQL     │
│  Servicio B  │────▶│  (image URLs)   │
│  (Room API)  │     └─────────────────┘
└──────────────┘              │
                              ▼
                        ┌───────────┐
                        │ SQL Server│
                        └───────────┘
```

### Responsibilities

| Concern | Owner |
|---------|-------|
| File upload / download / bucket management | **MinIO Service** (separate codebase) |
| Image metadata (hotel_images, room_images) | **Media Service** (this repo) |
| Object key generation & validation | **Media Service** |
| Pre-signed URL generation (future) | **Media Service** |
| Resizing / validation (future) | **Media Service** |

### Communication with MinIO Service

The Media Service does **not** use the MinIO Go SDK directly. Instead, it communicates with the MinIO Service through its REST API over HTTP:

| Action | Method | Endpoint | Description |
|--------|--------|----------|-------------|
| Upload | `POST` | `/upload` | Multipart form-data with `file` field. Returns `{ "bucket": "...", "key": "..." }` |
| Download | `GET` | `/download/{bucket}/{key}` | Streams the object with `Content-Type` and `Content-Disposition` headers |
| Health | `GET` | `/health` | Returns `{ "status": "ok" }` |
| Ready | `GET` | `/ready` | Returns `{ "status": "ready" }` |

The S3 HTTP client is implemented in [`app/internal/repo/S3_repo.go`](app/internal/repo/S3_repo.go) and uses Go's standard `net/http` package with piped streaming for uploads.

## Database Schema

### `hotel_images`

Stores S3 object references for hotel images. Object keys follow the pattern `hotels/{hotelID}/{timestamp}-{filename}`.

| Column | Type | Description |
|--------|------|-------------|
| `id` | `BIGSERIAL` | Primary key |
| `hotel_id` | `UUID` | Foreign key to hotel |
| `object_key` | `TEXT` | S3 object key (unique) |
| `content_type` | `TEXT` | MIME type |
| `file_size` | `BIGINT` | Size in bytes |
| `created_at` | `TIMESTAMPTZ` | Upload timestamp |

### `room_images`

Stores S3 object references for room images. Object keys follow the pattern `rooms/{roomID}/{timestamp}-{filename}`.

| Column | Type | Description |
|--------|------|-------------|
| `id` | `BIGSERIAL` | Primary key |
| `room_id` | `UUID` | Foreign key to room |
| `object_key` | `TEXT` | S3 object key (unique) |
| `content_type` | `TEXT` | MIME type |
| `file_size` | `BIGINT` | Size in bytes |
| `created_at` | `TIMESTAMPTZ` | Upload timestamp |

## Project Structure

```
.
├── app/
│   ├── cmd/api/
│   │   └── main.go                  # Application entry point
│   └── internal/
│       ├── config/config.go         # Configuration loading (YAML + env vars)
│       ├── database/
│       │   ├── db.go                # PostgreSQL connection pool (pgx)
│       │   └── migration.go         # Embedded migration runner (golang-migrate)
│       ├── handler/
│       │   ├── handlers.go          # HTTP handlers (upload, download, health)
│       │   ├── routing.go           # Chi router setup
│       │   └── middleware.go        # JWT auth, rate limiting, CORS, security headers
│       ├── helper/
│       │   ├── util.go              # Error responses, error mapping, env helpers
│       │   └── validator.go         # Request validation (go-playground/validator)
│       ├── logging/logger.go        # Structured JSON logger (slog + httplog)
│       ├── models/models.go         # Data models (DownloadResult, UploadResponse)
│       ├── repo/
│       │   ├── repo.go              # ServiceRepository + S3Repository interfaces
│       │   └── S3_repo.go           # S3 HTTP client (talks to MinIO Service)
│       └── service/service.go       # Business logic layer
├── app/sql/
│   ├── efs.go                       # Embedded filesystem for migrations
│   └── migrations/
│       ├── 001_create_table.up/down.sql
│       ├── 001_hotel.up/down.sql
│       └── 002_s3_images.up/down.sql
├── config.yaml                      # Service configuration
├── Dockerfile                       # Multi-stage Docker build
├── go.mod / go.sum                  # Go module definition
└── archived-service/                # Original monolithic codebase (preserved for reference)
```

## Configuration

The service is configured via [`config.yaml`](config.yaml) with environment variable expansion:

```yaml
server:
  host: "0.0.0.0"
  port: 8080

minio_service:
  url: "http://minio-service:8080"   # Base URL of the MinIO Service
  bucket: "media"                     # Default bucket name
```

### Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `DATABASE_URL` | Yes | PostgreSQL connection string (e.g. `postgres://user:pass@localhost:5432/media`) |
| `MINIO_ACCESS_KEY` | No | Only needed by the MinIO Service, not this service |
| `MINIO_SECRET_KEY` | No | Only needed by the MinIO Service, not this service |

## API Endpoints

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/health` | No | Liveness probe |
| `GET` | `/ready` | No | Readiness probe (checks DB) |
| `POST` | `/upload` | No | Upload a file (multipart form-data, `file` field) |
| `GET` | `/download/{bucket}/{key}` | No | Download a file by bucket and key |
| *TBD* | *Protected routes* | JWT | Future authenticated endpoints |

## Running

### Local Development

```bash
# Set required environment variables
export DATABASE_URL="postgres://user:pass@localhost:5432/media?sslmode=disable"

# Run the service
go run ./app/cmd/api
```

### Docker

```bash
docker build -t media-service .
docker run -e DATABASE_URL="postgres://..." media-service
```

## Dependencies

| Dependency | Purpose |
|------------|---------|
| `go-chi/chi/v5` | HTTP router |
| `go-chi/httplog/v3` | Structured HTTP logging |
| `golang-jwt/jwt/v5` | JWT authentication middleware |
| `golang-migrate/migrate/v4` | Database migrations |
| `jackc/pgx/v5` | PostgreSQL driver (connection pool) |
| `go-playground/validator/v10` | Request validation |
| `gopkg.in/yaml.v3` | YAML config parsing |

