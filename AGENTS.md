# Filehost - IRC File Hosting Service

## Overview

This is an implementation of the [draft/FILEHOST IRC specification](filehost.md), which provides a file hosting service for IRC users. The service allows authenticated IRC users to upload files via HTTP POST, which can then be shared on IRC channels.

## Architecture

The service is a standalone HTTP server written in Go that integrates with the Ergo IRC server for authentication.

### Key Components

1. **HTTP Server** (`lib/server.go`)
   - Handles upload and file retrieval endpoints
   - Middleware chain: panic recovery → logging → authentication → handlers
   - Supports both TCP and Unix domain sockets
   - Optional TLS support

2. **Storage Layer** (`lib/storage.go`)
   - Files stored in `{directory}/files/{id}{.ext}`
   - Metadata stored in `{directory}/metadata/{id}.json`
   - File IDs: 16 bytes from `crypto/rand`, encoded as base64url (22 characters)
   - Creates empty `index.html` in files directory to prevent enumeration when served by nginx

3. **Configuration** (`lib/config.go`)
   - YAML-based configuration
   - Custom duration parser supporting days, weeks, months, years (`lib/custime/`)

4. **Authentication**
   - HTTP Basic authentication (for IRC SASL PLAIN)
   - Validates credentials against Ergo API `/v1/check_auth` endpoint
   - OAUTHBEARER authentication: TODO (not yet implemented)

## File Structure

```
.
├── main.go                    # Entry point
├── config.yaml                # Example configuration (DO NOT EDIT for local testing)
├── test_config.yaml           # Local test configuration
├── lib/
│   ├── server.go              # HTTP server and handlers
│   ├── storage.go             # File storage and metadata management
│   ├── config.go              # Configuration loading
│   └── custime/
│       └── parseduration.go   # Custom duration parser (supports d, w, mo, y)
└── go.mod                     # Go module dependencies
```

## Building

```bash
go build -o filehost
```

Dependencies:
- `gopkg.in/yaml.v2` - YAML configuration parsing
- Standard library only (no external packages for core functionality)

## Running

```bash
./filehost config.yaml
```

For testing with a local Ergo instance:
```bash
./filehost test_config.yaml
```

## Configuration

See `config.yaml` for a fully documented example configuration.

Key settings:
- `server.listen-address`: Port or unix socket (e.g., `:8098` or `unix:/path/to/sock`)
- `server.tls`: Optional TLS certificate and key
- `server.paths.upload`: POST endpoint for uploads (default: `/upload`)
- `server.paths.files`: GET/HEAD endpoint for files (default: `/files`)
- `directory`: Base directory for file storage
- `ergo.api-url`: Ergo HTTP API endpoint
- `ergo.bearer-token`: Bearer token for Ergo API authentication

## API Endpoints

### `OPTIONS /upload`
- No authentication required
- Returns `Accept-Post` header indicating supported MIME types

### `POST /upload`
- **Authentication required**: HTTP Basic (username/password validated against Ergo API)
- Request headers:
  - `Content-Type`: MIME type of the file
  - `Content-Disposition`: Optional filename (e.g., `attachment; filename="image.jpg"`)
  - `Content-Length`: File size
- Response: `201 Created` with `Location` header pointing to uploaded file

### `GET /files/{id}{.ext}`
- No authentication required
- Returns the uploaded file with appropriate headers
- Response headers: `Content-Type`, `Content-Disposition`, `Content-Length`, `Last-Modified`

### `HEAD /files/{id}{.ext}`
- No authentication required
- Same as GET but without body

## File Storage

Each uploaded file generates:

1. **File**: `{directory}/files/{id}{.ext}`
   - ID: 22-character base64url string (16 random bytes)
   - Extension: derived from `Content-Type` or filename

2. **Metadata**: `{directory}/metadata/{id}.json`
   ```json
   {
     "content_type": "text/plain",
     "upload_time": "2026-02-15T07:55:37.588459214Z",
     "filename": "test.txt"
   }
   ```

## Authentication

The service uses the Ergo IRC server as the authentication source:

- Client sends HTTP Basic authentication: `Authorization: Basic <base64(username:password)>`
- Server validates against Ergo API: `POST /v1/check_auth`
- Ergo API request includes bearer token for API authentication
- Only POST requests require authentication; GET/HEAD are public

See [Ergo API documentation](~/workspace/ergo/docs/API.md) for details.

## Middleware Stack

Order: **panic → logging → auth → handlers**

1. **Panic Recovery** (`panicMiddleware`)
   - Catches panics, logs with stack trace
   - Returns 500 Internal Server Error
   - Based on `~/trash/panic.go`

2. **Logging** (`loggingMiddleware`)
   - Logs requests based on configured log level
   - Debug: request details + timing
   - Info: request details only

3. **Authentication** (`authMiddleware`)
   - GET/HEAD/OPTIONS: pass through
   - POST: require HTTP Basic auth
   - Validates credentials via `checkBasicAuth()`

## Testing

Test with curl:

```bash
# Upload a file (requires authentication)
echo "test content" | curl -X POST \
  -u "username:password" \
  -H "Content-Type: text/plain" \
  -H "Content-Disposition: attachment; filename=\"test.txt\"" \
  --data-binary @- \
  http://localhost:8098/upload

# Retrieve a file (no authentication)
curl http://localhost:8098/files/{id}.txt

# Check metadata (HEAD request)
curl -I http://localhost:8098/files/{id}.txt
```

Test authentication against local Ergo:
- Ensure Ergo is running with API enabled
- Update `test_config.yaml` with correct `bearer-token`
- Use valid IRC account credentials

## Security Considerations

1. **TLS**: Use TLS in production (configure `server.tls` section)
2. **Bearer Token**: Keep `ergo.bearer-token` secret (high-entropy, 32+ bytes)
3. **Directory Enumeration**: Empty `index.html` prevents listing when served by nginx
4. **File Expiration**: TODO - implement cleanup based on `limits.expiration`

## TODO

- [ ] Implement OAUTHBEARER authentication (see `authMiddleware` in `lib/server.go`)
- [ ] Implement file expiration/cleanup based on `limits.expiration`
- [ ] Add rate limiting for uploads
- [ ] Add file size limits
- [ ] Add MIME type restrictions (configurable accept list)

## Spec Compliance

This implementation follows the [draft/FILEHOST specification](~/trash/filehost.md):
- ✅ Advertises upload URI via ISUPPORT token (handled by Ergo)
- ✅ Accepts POST requests for uploads
- ✅ Returns 201 Created with Location header
- ✅ Supports OPTIONS requests with Accept-Post header
- ✅ Supports GET/HEAD requests for uploaded files
- ✅ Implements HTTP Basic authentication for SASL PLAIN
- ⏳ OAUTHBEARER authentication (TODO)

## Development Notes

- File IDs use `base64.RawURLEncoding` (URL-safe, no padding)
- No external dependencies beyond `gopkg.in/yaml.v2`
- `config.yaml` is the example file - use `test_config.yaml` for local testing
- The `custime` package extends Go's duration parser for human-readable intervals
- When reviewing branches, ignore the cleanliness of the individual commits and their messages, since we use a squash workflow
