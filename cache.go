package doclingclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/klauspost/compress/zstd"
)

// Cache stores and retrieves ConvertResponse payloads keyed by an opaque
// string. Implementations must be safe for concurrent use across goroutines
// within a process. Cross-process atomicity is provided by FileCache via
// rename-on-write.
type Cache interface {
	Get(key string) (resp *ConvertResponse, ok bool, err error)
	Put(key string, resp *ConvertResponse) error
}

// CacheKey returns a stable hex SHA-256 of the inputs that influence the
// conversion output. For local files, pass a synthetic Source whose
// Base64String is "sha256:<filehash>" instead of the actual base64 — see
// SourceForFile.
func CacheKey(sources []Source, opts *Options) string {
	h := sha256.New()
	enc := json.NewEncoder(h)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(struct {
		Sources []Source `json:"sources"`
		Options *Options `json:"options,omitempty"`
	}{sources, opts})
	return hex.EncodeToString(h.Sum(nil))
}

// HashFile returns a hex SHA-256 of the file at path. Used to fingerprint
// local files for cache keys without base64-encoding them.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// SourceForFile builds a synthetic Source for a local file suitable for
// CacheKey. It does NOT read the file contents into the Source — only its
// hash. Use this when keying a cache; for actual upload, use ConvertPath.
func SourceForFile(path string) (Source, error) {
	h, err := HashFile(path)
	if err != nil {
		return Source{}, err
	}
	return Source{
		Kind:         "file",
		Filename:     filepath.Base(path),
		Base64String: "sha256:" + h,
	}, nil
}

// DefaultCacheDir returns the XDG cache directory for doclingclient (typically
// ~/.cache/doclingclient or $XDG_CACHE_HOME/doclingclient).
func DefaultCacheDir() (string, error) {
	if dir := os.Getenv("XDG_CACHE_HOME"); dir != "" {
		return filepath.Join(dir, "doclingclient"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "doclingclient"), nil
}

// FileCache stores ConvertResponse payloads as zstd-compressed JSON in
// <Root>/<Version>/<key>.json.zst. Version is intended to be a short stable
// hash of the server's /version output, so that a new server release produces
// a fresh namespace without invalidating older ones — useful for diffing or
// pruning by version.
type FileCache struct {
	Root    string
	Version string
}

// NewFileCache prepares the namespace directory and returns a FileCache.
func NewFileCache(root, version string) (*FileCache, error) {
	if root == "" {
		return nil, fmt.Errorf("doclingclient: cache root is empty")
	}
	if version == "" {
		version = "unknown"
	}
	dir := filepath.Join(root, version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("doclingclient: cache: %w", err)
	}
	return &FileCache{Root: root, Version: version}, nil
}

// Dir returns the namespace directory (<Root>/<Version>).
func (c *FileCache) Dir() string { return filepath.Join(c.Root, c.Version) }

func (c *FileCache) path(key string) string {
	return filepath.Join(c.Dir(), key+".json.zst")
}

// Get returns the cached ConvertResponse for key, if any.
func (c *FileCache) Get(key string) (*ConvertResponse, bool, error) {
	f, err := os.Open(c.path(key))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer f.Close()

	dec, err := zstd.NewReader(f)
	if err != nil {
		return nil, false, err
	}
	defer dec.Close()

	var resp ConvertResponse
	if err := json.NewDecoder(dec).Decode(&resp); err != nil {
		return nil, false, fmt.Errorf("doclingclient: cache decode: %w", err)
	}
	return &resp, true, nil
}

// Put writes resp under key, replacing any existing entry atomically.
func (c *FileCache) Put(key string, resp *ConvertResponse) error {
	final := c.path(key)
	tmp, err := os.CreateTemp(filepath.Dir(final), ".tmp-*.zst")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}

	enc, err := zstd.NewWriter(tmp)
	if err != nil {
		cleanup()
		return err
	}
	if err := json.NewEncoder(enc).Encode(resp); err != nil {
		_ = enc.Close()
		cleanup()
		return err
	}
	if err := enc.Close(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, final)
}

// versionTTL bounds how long a cached /version result is reused before being
// refreshed. Long enough to make CLI invocations cheap, short enough to pick
// up a new server release within a day.
const versionTTL = 24 * time.Hour

const versionFileName = "_server_version.json"

type cachedVersion struct {
	FetchedAt time.Time      `json:"fetched_at"`
	Version   map[string]any `json:"version"`
}

// ServerVersion returns the docling-serve /version payload and a short stable
// hash of it suitable as a cache namespace. The /version result itself is
// cached on disk at <cacheDir>/_server_version.json for up to 24 h to avoid
// an extra round trip on every invocation.
func ServerVersion(ctx context.Context, c *Client, cacheDir string) (map[string]any, string, error) {
	if cacheDir != "" {
		if v, ok := readCachedVersion(cacheDir); ok {
			return v, versionHash(v), nil
		}
	}
	v, err := c.Version(ctx)
	if err != nil {
		return nil, "", err
	}
	if cacheDir != "" {
		writeCachedVersion(cacheDir, v)
	}
	return v, versionHash(v), nil
}

func readCachedVersion(cacheDir string) (map[string]any, bool) {
	b, err := os.ReadFile(filepath.Join(cacheDir, versionFileName))
	if err != nil {
		return nil, false
	}
	var cv cachedVersion
	if err := json.Unmarshal(b, &cv); err != nil {
		return nil, false
	}
	if time.Since(cv.FetchedAt) > versionTTL {
		return nil, false
	}
	return cv.Version, true
}

func writeCachedVersion(cacheDir string, v map[string]any) {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return
	}
	b, err := json.Marshal(cachedVersion{FetchedAt: time.Now(), Version: v})
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(cacheDir, versionFileName), b, 0o644)
}

// versionHash produces a deterministic short hex digest of a version map.
// Order-independent across map iteration: keys are sorted before hashing.
func versionHash(v map[string]any) string {
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		vb, _ := json.Marshal(v[k])
		_, _ = h.Write([]byte(k))
		_, _ = h.Write([]byte{':'})
		_, _ = h.Write(vb)
		_, _ = h.Write([]byte{';'})
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// WriteVersionInfo records the full version map alongside cached results, so
// `cat <cache-dir>/<hash>/_info.json` reveals which server build that
// namespace corresponds to.
func (c *FileCache) WriteVersionInfo(v map[string]any) error {
	if v == nil {
		return nil
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(c.Dir(), "_info.json"), b, 0o644)
}
