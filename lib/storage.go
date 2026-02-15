// Copyright (c) 2021 Shivaram Lingamneni <slingamn@cs.stanford.edu>
// released under the MIT license

package lib

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type FileMetadata struct {
	ContentType string    `json:"content_type"`
	UploadTime  time.Time `json:"upload_time"`
	FilePath    string    `json:"file_path"`
	Username    string    `json:"username"`
	Size        int64     `json:"size"`
	Extension   string    `json:"extension,omitempty"`
}

type Storage struct {
	baseDir     string
	filesDir    string
	metadataDir string
}

func NewStorage(baseDir string) (*Storage, error) {
	filesDir := filepath.Join(baseDir, "files")
	metadataDir := filepath.Join(baseDir, "metadata")

	// Create directories if they don't exist
	if err := os.MkdirAll(filesDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create files directory: %w", err)
	}
	if err := os.MkdirAll(metadataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create metadata directory: %w", err)
	}

	// Create empty index.html in files directory to prevent enumeration
	// when served via nginx or similar web servers
	indexPath := filepath.Join(filesDir, "index.html")
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		if err := os.WriteFile(indexPath, []byte(""), 0644); err != nil {
			return nil, fmt.Errorf("failed to create index.html: %w", err)
		}
	}

	return &Storage{
		baseDir:     baseDir,
		filesDir:    filesDir,
		metadataDir: metadataDir,
	}, nil
}

// StoreFile saves a file with a new random ID and returns the ID and file extension
func (s *Storage) StoreFile(reader io.Reader, contentType, filename, username string, size int64, maxFileSize int64) (string, string, error) {
	// Generate a new random ID from 16 bytes of crypto/rand
	var idBytes [16]byte
	rand.Read(idBytes[:])
	fileID := base64.RawURLEncoding.EncodeToString(idBytes[:])

	// Limit reader if max file size is configured
	var limitedReader io.Reader = reader
	if maxFileSize > 0 {
		limitedReader = io.LimitReader(reader, maxFileSize+1)
	}

	// Write the file initially without extension
	tempPath := filepath.Join(s.filesDir, fileID)
	outFile, err := os.Create(tempPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to create file: %w", err)
	}

	written, err := io.Copy(outFile, limitedReader)
	outFile.Close()
	if err != nil {
		os.Remove(tempPath)
		return "", "", fmt.Errorf("failed to write file: %w", err)
	}

	// Verify we didn't exceed the limit
	if maxFileSize > 0 && written > maxFileSize {
		os.Remove(tempPath)
		return "", "", fmt.Errorf("file exceeds size limit of %d bytes", maxFileSize)
	}

	// Detect content type from magic numbers if missing/generic
	isGenericType := contentType == "" ||
		contentType == "application/octet-stream" ||
		contentType == "application/x-www-form-urlencoded"

	if isGenericType {
		// Read first bytes from the saved file
		f, err := os.Open(tempPath)
		if err == nil {
			var header [512]byte
			n, _ := f.Read(header[:])
			f.Close()

			detected := detectContentType(header[:n])
			if detected != "" {
				contentType = detected
			} else if contentType == "" {
				contentType = "application/octet-stream"
			}
		} else if contentType == "" {
			contentType = "application/octet-stream"
		}
	}

	// Determine file extension from content type or filename
	var ext string
	if filename != "" {
		ext = filepath.Ext(filename)
	}
	if ext == "" && contentType != "" {
		// Try to get extension from MIME type
		exts, err := mime.ExtensionsByType(contentType)
		if err == nil && len(exts) > 0 {
			ext = exts[0]
		}
	}

	// Rename file if extension is needed
	var filePath string
	if ext != "" {
		filePath = filepath.Join(s.filesDir, fileID+ext)
		if err := os.Rename(tempPath, filePath); err != nil {
			os.Remove(tempPath)
			return "", "", fmt.Errorf("failed to rename file: %w", err)
		}
	} else {
		filePath = tempPath
	}

	// Store metadata
	metadata := FileMetadata{
		ContentType: contentType,
		UploadTime:  time.Now().UTC(),
		FilePath:    filepath.Base(filePath),
		Username:    username,
		Size:        size,
		Extension:   ext,
	}

	metadataPath := filepath.Join(s.metadataDir, fileID+".json")
	metadataFile, err := os.Create(metadataPath)
	if err != nil {
		os.Remove(filePath)
		return "", "", fmt.Errorf("failed to create metadata file: %w", err)
	}
	defer metadataFile.Close()

	encoder := json.NewEncoder(metadataFile)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(metadata); err != nil {
		os.Remove(filePath)
		os.Remove(metadataPath)
		return "", "", fmt.Errorf("failed to write metadata: %w", err)
	}

	return fileID, ext, nil
}

// GetFile retrieves a file by ID and extension
func (s *Storage) GetFile(fileID, ext string) (*os.File, *FileMetadata, error) {
	// Construct file path
	var filePath string
	if ext != "" {
		filePath = filepath.Join(s.filesDir, fileID+ext)
	} else {
		filePath = filepath.Join(s.filesDir, fileID)
	}

	// Check if file exists
	file, err := os.Open(filePath)
	if err != nil {
		return nil, nil, err
	}

	// Load metadata
	metadataPath := filepath.Join(s.metadataDir, fileID+".json")
	metadataFile, err := os.Open(metadataPath)
	if err != nil {
		file.Close()
		return nil, nil, err
	}
	defer metadataFile.Close()

	var metadata FileMetadata
	if err := json.NewDecoder(metadataFile).Decode(&metadata); err != nil {
		file.Close()
		return nil, nil, err
	}

	return file, &metadata, nil
}

// FileExists checks if a file with the given ID and extension exists
func (s *Storage) FileExists(fileID, ext string) bool {
	var filePath string
	if ext != "" {
		filePath = filepath.Join(s.filesDir, fileID+ext)
	} else {
		filePath = filepath.Join(s.filesDir, fileID)
	}

	_, err := os.Stat(filePath)
	return err == nil
}

// ExtractFilename extracts the filename from Content-Disposition header
func ExtractFilename(contentDisposition string) string {
	if contentDisposition == "" {
		return ""
	}

	// Parse Content-Disposition header
	_, params, err := mime.ParseMediaType(contentDisposition)
	if err != nil {
		return ""
	}

	if filename, ok := params["filename"]; ok {
		return filepath.Base(filename)
	}

	return ""
}

// CleanExtension ensures the extension starts with a dot
func CleanExtension(ext string) string {
	if ext == "" {
		return ""
	}
	if !strings.HasPrefix(ext, ".") {
		return "." + ext
	}
	return ext
}

// detectContentType detects the content type from magic number headers
func detectContentType(data []byte) string {
	// JPEG: FF D8 FF
	if len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "image/jpeg"
	}

	// PNG: 89 50 4E 47 0D 0A 1A 0A
	if len(data) >= 8 && data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 &&
		data[4] == 0x0D && data[5] == 0x0A && data[6] == 0x1A && data[7] == 0x0A {
		return "image/png"
	}

	// GIF: "GIF87a" or "GIF89a"
	if len(data) >= 6 && string(data[0:3]) == "GIF" &&
		(string(data[0:6]) == "GIF87a" || string(data[0:6]) == "GIF89a") {
		return "image/gif"
	}

	// PDF: "%PDF"
	if len(data) >= 4 && string(data[0:4]) == "%PDF" {
		return "application/pdf"
	}

	return ""
}

// StartCleanup starts a goroutine that periodically deletes expired files
func (s *Storage) StartCleanup(expiration time.Duration, logger *log.Logger) {
	go func() {
		// Run cleanup immediately on startup
		s.cleanup(expiration, logger)

		// Then run every hour
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()

		for range ticker.C {
			s.cleanup(expiration, logger)
		}
	}()
}

// cleanup scans metadata files and deletes expired ones
func (s *Storage) cleanup(expiration time.Duration, logger *log.Logger) {
	defer HandlePanic(nil)

	logger.Printf("Starting cleanup job (expiration: %v)", expiration)

	// Read all metadata files
	entries, err := os.ReadDir(s.metadataDir)
	if err != nil {
		logger.Printf("Error reading metadata directory: %v", err)
		return
	}

	now := time.Now()
	deleted := 0
	errors := 0

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		// Read metadata file
		metadataPath := filepath.Join(s.metadataDir, entry.Name())
		metadataFile, err := os.Open(metadataPath)
		if err != nil {
			logger.Printf("Error opening metadata file %s: %v", entry.Name(), err)
			errors++
			continue
		}

		var metadata FileMetadata
		err = json.NewDecoder(metadataFile).Decode(&metadata)
		metadataFile.Close()
		if err != nil {
			logger.Printf("Error decoding metadata file %s: %v", entry.Name(), err)
			errors++
			continue
		}

		// Check if file is expired
		if now.Sub(metadata.UploadTime) > expiration {
			// Construct file path from stored filename
			filePath := filepath.Join(s.filesDir, metadata.FilePath)
			fileID := strings.TrimSuffix(entry.Name(), ".json")

			// Delete the file
			if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
				logger.Printf("Error deleting file %s: %v", filePath, err)
				errors++
			}

			// Delete the metadata file
			if err := os.Remove(metadataPath); err != nil && !os.IsNotExist(err) {
				logger.Printf("Error deleting metadata %s: %v", metadataPath, err)
				errors++
			} else {
				deleted++
				logger.Printf("Deleted expired file: %s (uploaded: %v, age: %v)",
					fileID, metadata.UploadTime.Format(time.RFC3339), now.Sub(metadata.UploadTime))
			}
		}
	}

	logger.Printf("Cleanup job completed: %d files deleted, %d errors", deleted, errors)
}
