package zentrox

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// StaticOptions controls behavior of Static(...)
type StaticOptions struct {
	// Directory on disk to serve from (absolute or relative to process cwd).
	Dir string
	// Optional index filename to serve when requesting the prefix root (e.g. "index.html").
	Index string
	// If true, do not auto-serve index when the request equals the prefix.
	DisableIndex bool
	// If non-zero, sets "Cache-Control: public, max-age=<seconds>" (otherwise no-cache).
	MaxAge time.Duration
	// If true, use strong ETag (SHA1 of content). Otherwise weak ETag (size-modtime).
	UseStrongETag bool
	// Optional allow-list of file extensions (lowercase, with dot), e.g. []string{".css",".js",".png"}.
	AllowedExt []string
	// If true, allow symlinks under Dir. By default symlink targets must resolve inside Dir.
	FollowSymlinks bool
}

// Static mounts a read-only file server under a prefix.
func (a *App) Static(prefix string, opt StaticOptions) {
	if prefix == "" || prefix[0] != '/' {
		panic("Static: prefix must start with '/'")
	}
	if opt.Dir == "" {
		panic("Static: Dir is required")
	}
	if len(prefix) > 1 && strings.HasSuffix(prefix, "/") {
		prefix = strings.TrimRight(prefix, "/")
	}

	root, err := filepath.Abs(opt.Dir)
	if err != nil {
		panic("Static: cannot resolve directory: " + err.Error())
	}
	allow := buildAllowedExtMap(opt.AllowedExt)

	pat := prefix + "/*filepath"
	rootPath := prefix
	h := func(c *Context) {
		rel := c.Param("filepath")
		target, fi, errCode, errMsg := resolveStaticTarget(root, rel, &opt, allow)
		if errCode != 0 {
			_ = c.String(errCode, "%s", errMsg)
			return
		}

		notModified := handleETagAndCache(c, target, fi, &opt)
		if notModified {
			c.Writer.WriteHeader(http.StatusNotModified)
			return
		}

		if ct := mime.TypeByExtension(filepath.Ext(target)); ct != "" {
			c.SetHeader(HeaderContentType, ct)
		}

		if c.Request.Method == http.MethodHead {
			c.Writer.WriteHeader(http.StatusOK)
			return
		}

		serveStaticFile(c, target)
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	a.onLocked(http.MethodGet, pat, h)
	a.onLocked(http.MethodGet, rootPath, h)
	a.onLocked(http.MethodHead, pat, h)
	a.onLocked(http.MethodHead, rootPath, h)
}

func buildAllowedExtMap(exts []string) map[string]struct{} {
	allow := make(map[string]struct{}, len(exts))
	for _, e := range exts {
		e = strings.ToLower(strings.TrimSpace(e))
		if e != "" && e[0] == '.' {
			allow[e] = struct{}{}
		}
	}
	return allow
}

func resolveStaticTarget(root, rel string, opt *StaticOptions, allow map[string]struct{}) (string, os.FileInfo, int, string) {
	if rel == "" || rel == "/" {
		if !opt.DisableIndex && opt.Index != "" {
			rel = "/" + opt.Index
		} else {
			return "", nil, http.StatusNotFound, MsgNotFound
		}
	}

	clean := filepath.Clean(rel)
	if strings.HasPrefix(clean, "..") {
		return "", nil, http.StatusForbidden, MsgForbidden
	}
	target := filepath.Join(root, strings.TrimPrefix(clean, string(filepath.Separator)))
	if !isWithinBase(root, target) {
		return "", nil, http.StatusForbidden, MsgForbidden
	}

	fi, err := os.Stat(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil, http.StatusNotFound, MsgNotFound
		}
		return "", nil, http.StatusInternalServerError, MsgStatError
	}
	if fi.IsDir() {
		if !opt.DisableIndex && opt.Index != "" {
			target = filepath.Join(target, opt.Index)
			fi, err = os.Stat(target)
			if err != nil || fi.IsDir() {
				return "", nil, http.StatusNotFound, MsgNotFound
			}
		} else {
			return "", nil, http.StatusNotFound, MsgNotFound
		}
	}

	if !opt.FollowSymlinks && !isWithinBaseResolved(root, target) {
		return "", nil, http.StatusForbidden, MsgForbidden
	}

	if len(allow) > 0 {
		ext := strings.ToLower(filepath.Ext(target))
		if _, ok := allow[ext]; !ok {
			return "", nil, http.StatusForbidden, MsgForbidden
		}
	}

	return target, fi, 0, ""
}

func handleETagAndCache(c *Context, target string, fi os.FileInfo, opt *StaticOptions) bool {
	etag, lastMod := "", fi.ModTime().UTC()
	if opt.UseStrongETag {
		if sum, err := sha1File(target); err == nil {
			etag = `"` + hex.EncodeToString(sum) + `"`
		}
	} else {
		etag = `W/"` + strconv.FormatInt(fi.Size(), 10) + "-" + strconv.FormatInt(lastMod.Unix(), 10) + `"`
	}
	if etag != "" {
		c.SetHeader(HeaderETag, etag)
	}
	c.SetHeader(HeaderLastModified, lastMod.Format(http.TimeFormat))

	if opt.MaxAge > 0 {
		sec := int(opt.MaxAge / time.Second)
		c.SetHeader(HeaderCacheControl, "public, max-age="+strconv.Itoa(sec))
	} else {
		c.SetHeader(HeaderCacheControl, CacheControlNoCache)
	}

	if inm := c.GetHeader(HeaderIfNoneMatch); inm != "" && etag != "" {
		if etagMatch(inm, etag) {
			return true
		}
	}
	if ims := c.GetHeader(HeaderIfModifiedSince); ims != "" {
		if t, err := time.Parse(http.TimeFormat, ims); err == nil {
			if !lastMod.After(t) {
				return true
			}
		}
	}
	return false
}

func serveStaticFile(c *Context, target string) {
	f, err := os.Open(target)
	if err != nil {
		_ = c.String(http.StatusInternalServerError, MsgOpenError)
		return
	}
	defer f.Close()

	c.Writer.WriteHeader(http.StatusOK)
	if _, err := io.Copy(c.Writer, f); err != nil {
		c.SetError(err)
	}
}

func isWithinBase(base, child string) bool {
	b, err := filepath.Abs(base)
	if err != nil {
		return false
	}
	c, err := filepath.Abs(child)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(b, c)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func isWithinBaseResolved(base, child string) bool {
	b, err := filepath.EvalSymlinks(base)
	if err != nil {
		return false
	}
	c, err := filepath.EvalSymlinks(child)
	if err != nil {
		return false
	}
	return isWithinBase(b, c)
}

func sha1File(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

func etagMatch(header, etag string) bool {
	parts := strings.Split(header, ",")
	for _, p := range parts {
		if strings.TrimSpace(p) == etag {
			return true
		}
	}
	return false
}
