package zentrox

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// UploadOptions controls how files are accepted and saved.
type UploadOptions struct {
	// Maximum memory used by ParseMultipartForm; files larger than this are stored in temporary files.
	MaxMemory int64 // default 10 << 20 (10 MiB)
	// Allowed file extensions (lowercase, with dot). Empty means allow all.
	AllowedExt []string
	// If true, sanitize the base filename (only [a-zA-Z0-9._-]) to avoid weird characters.
	Sanitize bool
	// If true, always generate a unique filename (timestamp + random suffix).
	GenerateUniqueName bool
	// If false and file exists, returns error. If true, overwrite existing file.
	Overwrite bool
}

// SaveUploadedFile reads file from multipart form by field name and writes it into dstDir.
// It validates extension (if provided), prevents path traversal, and can sanitize/generate names.
// Returns the full path saved to.
func (c *Context) SaveUploadedFile(field, dstDir string, opt UploadOptions) (string, error) {
	if dstDir == "" {
		return "", errors.New("upload: destination directory required")
	}
	if opt.MaxMemory <= 0 {
		opt.MaxMemory = 10 << 20 // 10 MiB
	}
	if err := c.Request.ParseMultipartForm(opt.MaxMemory); err != nil {
		return "", err
	}
	file, hdr, err := c.Request.FormFile(field)
	if err != nil {
		return "", err
	}
	defer file.Close()

	name := hdr.Filename
	if opt.Sanitize {
		name = sanitizeFilename(name)
	}
	if opt.GenerateUniqueName {
		ext := strings.ToLower(filepath.Ext(name))
		base := strings.TrimSuffix(name, ext)
		name = base + "-" + time.Now().UTC().Format("20060102T150405") + "-" + randomHex(4) + ext
	}
	if name == "" {
		return "", errors.New("upload: empty filename")
	}

	if len(opt.AllowedExt) > 0 {
		ext := strings.ToLower(filepath.Ext(name))
		allowed := false
		for _, e := range opt.AllowedExt {
			if strings.ToLower(e) == ext {
				allowed = true
				break
			}
		}
		if !allowed {
			return "", errors.New("upload: disallowed file extension")
		}
	}

	dstRoot, err := filepath.Abs(dstDir)
	if err != nil {
		return "", err
	}

	target := filepath.Join(dstRoot, filepath.Base(name))
	if ok := isWithinBase(dstRoot, target); !ok {
		return "", errors.New("upload: invalid path")
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	realDstRoot, err := filepath.EvalSymlinks(dstRoot)
	if err != nil {
		return "", err
	}
	realTargetDir, err := filepath.EvalSymlinks(filepath.Dir(target))
	if err != nil {
		return "", err
	}
	if !isWithinBase(realDstRoot, realTargetDir) {
		return "", errors.New("upload: invalid destination")
	}

	if fi, err := os.Lstat(target); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("upload: refusing to overwrite symlink")
		}
		if fi.IsDir() {
			return "", errors.New("upload: target is a directory")
		}
		if !opt.Overwrite {
			return "", errors.New("upload: file exists")
		}
		if err := os.Remove(target); err != nil {
			return "", err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		return "", err
	}

	return target, nil
}

// UploadedFile returns the multipart file and header for advanced use.
// Caller must close the returned multipart.File.
func (c *Context) UploadedFile(field string, maxMemory int64) (multipart.File, *multipart.FileHeader, error) {
	if maxMemory <= 0 {
		maxMemory = 10 << 20
	}
	if err := c.Request.ParseMultipartForm(maxMemory); err != nil {
		return nil, nil, err
	}
	return c.Request.FormFile(field)
}

func randomHex(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "random"
	}
	return hex.EncodeToString(b)
}

var sanitizeFilenameRe = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func sanitizeFilename(name string) string {
	name = filepath.Base(name)
	name = sanitizeFilenameRe.ReplaceAllString(name, "_")
	if name == "" || name == "." || name == ".." {
		name = "file"
	}
	return name
}
