// Copyright (c) 2021 Shivaram Lingamneni <slingamn@cs.stanford.edu>
// released under the MIT license

package lib

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"
)

// HTTP client for API requests with 10 second timeout
var httpClient = &http.Client{Timeout: 10 * time.Second}

type Server struct {
	config  *Config
	storage *Storage
	logger  *log.Logger
	commit  string
	version string
}

func NewServer(config *Config, commit, version string) (*Server, error) {
	storage, err := NewStorage(config.Directory)
	if err != nil {
		return nil, err
	}

	logger := log.New(os.Stdout, "", log.LstdFlags)

	return &Server{
		config:  config,
		storage: storage,
		logger:  logger,
		commit:  commit,
		version: version,
	}, nil
}

func (s *Server) Run() {
	// Start cleanup goroutine if expiration is configured
	expiration := time.Duration(s.config.Limits.Expiration)
	if expiration > 0 {
		s.storage.StartCleanup(expiration, s.logger)
		s.logger.Printf("Started cleanup job (expiration: %v, interval: 1h)", expiration)
	} else {
		s.logger.Println("File expiration disabled (no cleanup job)")
	}

	mux := http.NewServeMux()

	// Register handlers
	mux.HandleFunc(s.config.Server.Paths.Upload, s.handleUpload)
	mux.HandleFunc(s.config.Server.Paths.Files+"/", s.handleFiles)

	// Create HTTP server with middleware chain
	// Order: panic → logging → handlers
	handler := s.panicMiddleware(s.loggingMiddleware(mux))
	server := &http.Server{
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	// Determine listener
	var listener net.Listener
	var err error

	listenAddr := s.config.Server.ListenAddress

	if strings.HasPrefix(listenAddr, "unix:") {
		// Unix domain socket
		socketPath := strings.TrimPrefix(listenAddr, "unix:")
		// Remove existing socket file if it exists
		os.Remove(socketPath)
		listener, err = net.Listen("unix", socketPath)
		if err != nil {
			s.logger.Fatal("Failed to listen on unix socket: ", err)
		}
		s.logger.Printf("Listening on unix socket: %s", socketPath)
	} else {
		// TCP listener
		listener, err = net.Listen("tcp", listenAddr)
		if err != nil {
			s.logger.Fatal("Failed to listen on TCP: ", err)
		}
		s.logger.Printf("Listening on: %s", listenAddr)
	}

	// Start server with or without TLS
	if s.config.Server.TLS != nil {
		s.logger.Println("Using TLS")
		var certStore AutoreloadingCertStore
		if err = certStore.Initialize(s.config.Server.TLS.Cert, s.config.Server.TLS.Key, time.Minute); err != nil {
			s.logger.Fatal("TLS load error: ", err)
		}
		tlsConfig := certStore.TLSConfig()
		tlsConfig.ClientAuth = tls.RequestClientCert
		tlsListener := tls.NewListener(listener, tlsConfig)
		err = server.Serve(tlsListener)
	} else {
		s.logger.Println("Using plaintext (no TLS)")
		err = server.Serve(listener)
	}

	if err != nil && err != http.ErrServerClosed {
		s.logger.Fatal("Server error: ", err)
	}
}

// panicMiddleware recovers from panics in HTTP handlers
func (s *Server) panicMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if r := recover(); r != nil {
				s.logger.Printf("Panic encountered: %v\n%s", r, debug.Stack())
				http.Error(w, "Internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// loggingMiddleware logs all HTTP requests
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Log request
		if s.config.Logging == "debug" || s.config.Logging == "info" {
			s.logger.Printf("%s %s %s", r.Method, r.URL.Path, r.RemoteAddr)
		}

		next.ServeHTTP(w, r)

		// Log response time
		if s.config.Logging == "debug" {
			s.logger.Printf("Request completed in %v", time.Since(start))
		}
	})
}

// authenticate validates credentials and returns the username, or an empty string on failure
func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) (string, bool) {
	// Try client certificate authentication first (no Authorization header needed)
	if certfp, peerCerts, err := GetCertFP(r); err == nil && certfp != "" {
		certAuth := APIInput{
			Certfp:    certfp,
			PeerCerts: make([]string, len(peerCerts)),
		}
		for i, cert := range peerCerts {
			certAuth.PeerCerts[i] = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}))
		}
		ok, accountName := s.checkErgoAuth(certAuth)
		if ok {
			return accountName, true
		}
		// cert present but not recognized — fall through to try other auth methods
	}

	// Parse Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		w.Header().Set("WWW-Authenticate", `Basic realm="filehost"`)
		http.Error(w, "Authorization required", http.StatusUnauthorized)
		return "", false
	}

	// Check if it's Basic authentication (for SASL PLAIN)
	if strings.HasPrefix(authHeader, "Basic ") {
		// Extract username and password
		username, password, ok := r.BasicAuth()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="filehost"`)
			http.Error(w, "Invalid authorization header", http.StatusUnauthorized)
			return "", false
		}

		// Validate credentials against Ergo API
		basicAuth := APIInput{
			AccountName: username,
			Passphrase:  password,
		}
		ok, accountName := s.checkErgoAuth(basicAuth)
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="filehost"`)
			http.Error(w, "Invalid credentials", http.StatusUnauthorized)
			return "", false
		}

		// Authentication successful
		return accountName, true
	}

	// Check if it's Bearer authentication (for SASL OAUTHBEARER)
	if strings.HasPrefix(authHeader, "Bearer ") {
		// TODO: Implement OAUTHBEARER authentication
		// Extract token from "Bearer <token>" header
		// Validate token against Ergo API
		http.Error(w, "Bearer authentication not yet implemented", http.StatusNotImplemented)
		return "", false
	}

	// Unknown authentication scheme
	w.Header().Set("WWW-Authenticate", `Basic realm="filehost"`)
	http.Error(w, "Unsupported authorization scheme", http.StatusUnauthorized)
	return "", false
}

type APIInput struct {
	AccountName string   `json:"accountName,omitempty"`
	Passphrase  string   `json:"passphrase,omitempty"`
	Certfp      string   `json:"certfp,omitempty"`
	PeerCerts   []string `json:"peerCerts,omitempty"`
	IP          string   `json:"ip,omitempty"`
	//OAuthBearer *oauth2.OAuthBearerOptions `json:"oauth2,omitempty"`
}

type APIOutput struct {
	AccountName string `json:"accountName"`
	Success     bool   `json:"success"`
	Error       string `json:"error"`
}

// checkBasicAuth validates username and password against the Ergo API
func (s *Server) checkErgoAuth(apiRequest APIInput) (bool, string) {
	// Prepare request body
	reqJSON, err := json.Marshal(apiRequest)
	if err != nil {
		s.logger.Printf("Error marshaling auth request: %v", err)
		return false, ""
	}

	// Make request to Ergo API
	apiURL := s.config.Ergo.APIURL + "/v1/check_auth"
	req, err := http.NewRequest("POST", apiURL, bytes.NewReader(reqJSON))
	if err != nil {
		s.logger.Printf("Error creating auth request: %v", err)
		return false, ""
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.config.Ergo.BearerToken)

	// Send request
	resp, err := httpClient.Do(req)
	if err != nil {
		s.logger.Printf("Error sending auth request: %v", err)
		return false, ""
	}
	defer resp.Body.Close()

	// Check HTTP status code
	if resp.StatusCode != http.StatusOK {
		s.logger.Printf("Auth API returned non-200 status: %d", resp.StatusCode)
		return false, ""
	}

	// Parse response
	var result APIOutput
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		s.logger.Printf("Error decoding auth response: %v", err)
		return false, ""
	}

	if result.Success {
		s.logger.Printf("Authentication successful for user: %s", result.AccountName)
	} else {
		s.logger.Printf("Authentication failed for user: %s", apiRequest.AccountName)
	}

	return result.Success, result.AccountName
}

// handleUpload handles both OPTIONS and POST requests to the upload endpoint
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodOptions:
		s.handleOptions(w, r)
	case http.MethodPost:
		s.handlePost(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleOptions handles OPTIONS requests
func (s *Server) handleOptions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Allow", "OPTIONS, POST")
	// Accept all content types for now
	w.Header().Set("Accept-Post", "*/*")
	w.WriteHeader(http.StatusNoContent)
}

// handlePost handles POST requests to upload files
func (s *Server) handlePost(w http.ResponseWriter, r *http.Request) {
	// Authenticate the request
	username, ok := s.authenticate(w, r)
	if !ok {
		// authenticate() already sent the error response
		return
	}

	// Get Content-Type
	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// Get filename from Content-Disposition if provided
	contentDisposition := r.Header.Get("Content-Disposition")
	filename := ExtractFilename(contentDisposition)

	// Get Content-Length (file size)
	size := r.ContentLength
	if size == 0 {
		http.Error(w, "Empty file", http.StatusBadRequest)
		return
	}

	// Check file size limit if configured
	if s.config.Limits.MaxFileSize > 0 && size > s.config.Limits.MaxFileSize {
		http.Error(w, fmt.Sprintf("File too large (max: %d bytes)", s.config.Limits.MaxFileSize),
			http.StatusRequestEntityTooLarge)
		return
	}

	// Store the file (with size limit enforcement)
	fileID, ext, err := s.storage.StoreFile(r.Body, contentType, filename, username, size, s.config.Limits.MaxFileSize)
	if err != nil {
		s.logger.Printf("Error storing file: %v", err)
		http.Error(w, "Failed to store file", http.StatusInternalServerError)
		return
	}

	// Construct the file URL
	var fileURL string
	var basePath string

	// Use external files URL if configured, otherwise use local path
	if s.config.Server.Paths.FilesURL != "" {
		basePath = s.config.Server.Paths.FilesURL
	} else {
		basePath = s.config.Server.Paths.Files
	}

	if ext != "" {
		fileURL = basePath + "/" + fileID + ext
	} else {
		fileURL = basePath + "/" + fileID
	}

	// Return 201 Created with Location header
	w.Header().Set("Location", fileURL)
	w.WriteHeader(http.StatusCreated)

	s.logger.Printf("File uploaded successfully: %s (ID: %s)", filename, fileID)
}

// handleFiles handles GET and HEAD requests for uploaded files
func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract file ID and extension from the path
	// Path format: /files/{id}{.ext}
	path := strings.TrimPrefix(r.URL.Path, s.config.Server.Paths.Files+"/")
	if path == "" {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	// Split the filename into ID and extension
	var fileID, ext string
	extIdx := strings.LastIndex(path, ".")
	if extIdx > 0 {
		fileID = path[:extIdx]
		ext = path[extIdx:]
	} else {
		fileID = path
		ext = ""
	}

	// Validate file ID format (base64url without padding, 22 chars for 16 bytes)
	if !isValidFileID(fileID) {
		http.Error(w, "Invalid file ID", http.StatusBadRequest)
		return
	}

	// Get the file
	file, metadata, err := s.storage.GetFile(fileID, ext)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "File not found", http.StatusNotFound)
		} else {
			s.logger.Printf("Error retrieving file: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}
	defer file.Close()

	// Get file info for Content-Length
	fileInfo, err := file.Stat()
	if err != nil {
		s.logger.Printf("Error getting file info: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Set headers
	if metadata.ContentType != "" {
		w.Header().Set("Content-Type", metadata.ContentType)
	}
	if metadata.FilePath != "" {
		disposition := fmt.Sprintf("attachment; filename=\"%s\"", filepath.Base(metadata.FilePath))
		w.Header().Set("Content-Disposition", disposition)
	}
	w.Header().Set("Content-Length", fmt.Sprintf("%d", fileInfo.Size()))
	w.Header().Set("Last-Modified", metadata.UploadTime.Format(http.TimeFormat))

	// For HEAD requests, don't send the body
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Send the file
	w.WriteHeader(http.StatusOK)
	io.Copy(w, file)
}

// isValidFileID performs basic validation for base64url-encoded file IDs
func isValidFileID(s string) bool {
	// base64.RawURLEncoding encodes 16 bytes as 22 characters
	if len(s) != 22 {
		return false
	}
	// Check valid base64url characters (A-Z, a-z, 0-9, -, _)
	for _, c := range s {
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
