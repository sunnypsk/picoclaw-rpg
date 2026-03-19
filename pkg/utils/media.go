package utils

import (
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/h2non/filetype"

	"github.com/sipeed/picoclaw/pkg/logger"
)

// IsAudioFile checks if a file is an audio file based on its filename extension and content type.
func IsAudioFile(filename, contentType string) bool {
	audioExtensions := []string{".mp3", ".wav", ".ogg", ".m4a", ".flac", ".aac", ".wma"}
	audioTypes := []string{"audio/", "application/ogg", "application/x-ogg"}

	for _, ext := range audioExtensions {
		if strings.HasSuffix(strings.ToLower(filename), ext) {
			return true
		}
	}

	for _, audioType := range audioTypes {
		if strings.HasPrefix(strings.ToLower(contentType), audioType) {
			return true
		}
	}

	return false
}

// InferMediaType determines the logical media kind from a filename and MIME content type.
// It returns one of "image", "audio", "video", or "file".
func InferMediaType(filename, contentType string) string {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	fn := strings.ToLower(strings.TrimSpace(filename))

	if strings.HasPrefix(ct, "image/") {
		return "image"
	}
	if strings.HasPrefix(ct, "audio/") || ct == "application/ogg" {
		return "audio"
	}
	if strings.HasPrefix(ct, "video/") {
		return "video"
	}

	switch filepath.Ext(fn) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".svg":
		return "image"
	case ".mp3", ".wav", ".ogg", ".m4a", ".flac", ".aac", ".wma", ".opus":
		return "audio"
	case ".mp4", ".avi", ".mov", ".webm", ".mkv":
		return "video"
	default:
		return "file"
	}
}

func PreferredExtensionForContentType(contentType string) string {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if ct == "" {
		return ""
	}

	if parsed, _, err := mime.ParseMediaType(ct); err == nil {
		ct = parsed
	}

	switch ct {
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/ogg", "application/ogg", "application/x-ogg":
		return ".ogg"
	case "audio/wav", "audio/x-wav", "audio/wave":
		return ".wav"
	case "audio/mp4", "audio/x-m4a", "audio/m4a":
		return ".m4a"
	case "audio/flac", "audio/x-flac":
		return ".flac"
	case "audio/aac", "audio/x-aac":
		return ".aac"
	case "audio/opus":
		return ".opus"
	}

	exts, err := mime.ExtensionsByType(ct)
	if err != nil || len(exts) == 0 {
		return ""
	}
	return strings.ToLower(exts[0])
}

func PreferredExtensionForBytes(data []byte) string {
	if len(data) == 0 {
		return ""
	}

	kind, err := filetype.Match(data)
	if err != nil || kind == filetype.Unknown {
		return ""
	}

	return PreferredExtensionForContentType(kind.MIME.Value)
}

// SanitizeFilename removes potentially dangerous characters from a filename
// and returns a safe version for local filesystem storage.
func SanitizeFilename(filename string) string {
	// Get the base filename without path
	base := filepath.Base(filename)

	// Remove any directory traversal attempts
	base = strings.ReplaceAll(base, "..", "")
	base = strings.ReplaceAll(base, "/", "_")
	base = strings.ReplaceAll(base, "\\", "_")

	return base
}

// DownloadOptions holds optional parameters for downloading files
type DownloadOptions struct {
	Timeout      time.Duration
	ExtraHeaders map[string]string
	LoggerPrefix string
	ProxyURL     string
}

// DownloadFile downloads a file from URL to a local temp directory.
// Returns the local file path or empty string on error.
func DownloadFile(urlStr, filename string, opts DownloadOptions) string {
	// Set defaults
	if opts.Timeout == 0 {
		opts.Timeout = 60 * time.Second
	}
	if opts.LoggerPrefix == "" {
		opts.LoggerPrefix = "utils"
	}

	mediaDir := filepath.Join(os.TempDir(), "picoclaw_media")
	if err := os.MkdirAll(mediaDir, 0o700); err != nil {
		logger.ErrorCF(opts.LoggerPrefix, "Failed to create media directory", map[string]any{
			"error": err.Error(),
		})
		return ""
	}

	// Generate unique filename with UUID prefix to prevent conflicts
	safeName := SanitizeFilename(filename)
	localPath := filepath.Join(mediaDir, uuid.New().String()[:8]+"_"+safeName)

	// Create HTTP request
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		logger.ErrorCF(opts.LoggerPrefix, "Failed to create download request", map[string]any{
			"error": err.Error(),
		})
		return ""
	}

	// Add extra headers (e.g., Authorization for Slack)
	for key, value := range opts.ExtraHeaders {
		req.Header.Set(key, value)
	}

	client := &http.Client{Timeout: opts.Timeout}
	if opts.ProxyURL != "" {
		proxyURL, parseErr := url.Parse(opts.ProxyURL)
		if parseErr != nil {
			logger.ErrorCF(opts.LoggerPrefix, "Invalid proxy URL for download", map[string]any{
				"error": parseErr.Error(),
				"proxy": opts.ProxyURL,
			})
			return ""
		}
		client.Transport = &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		logger.ErrorCF(opts.LoggerPrefix, "Failed to download file", map[string]any{
			"error": err.Error(),
			"url":   urlStr,
		})
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.ErrorCF(opts.LoggerPrefix, "File download returned non-200 status", map[string]any{
			"status": resp.StatusCode,
			"url":    urlStr,
		})
		return ""
	}

	out, err := os.Create(localPath)
	if err != nil {
		logger.ErrorCF(opts.LoggerPrefix, "Failed to create local file", map[string]any{
			"error": err.Error(),
		})
		return ""
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		os.Remove(localPath)
		logger.ErrorCF(opts.LoggerPrefix, "Failed to write file", map[string]any{
			"error": err.Error(),
		})
		return ""
	}

	logger.DebugCF(opts.LoggerPrefix, "File downloaded successfully", map[string]any{
		"path": localPath,
	})

	return localPath
}

// DownloadFileSimple is a simplified version of DownloadFile without options
func DownloadFileSimple(url, filename string) string {
	return DownloadFile(url, filename, DownloadOptions{
		LoggerPrefix: "media",
	})
}
