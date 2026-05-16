package doclingclient

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestCacheKey_Deterministic(t *testing.T) {
	src := []Source{NewHTTPSource("https://example.org/a.pdf")}
	opts := &Options{ToFormats: []string{"md", "json"}, DoOCR: Ptr(true)}

	a := CacheKey(src, opts)
	b := CacheKey(src, opts)
	if a != b {
		t.Fatalf("CacheKey not deterministic: %s vs %s", a, b)
	}
	if len(a) != 64 {
		t.Errorf("expected 64-hex-char key, got %d chars", len(a))
	}
}

func TestCacheKey_SensitiveToInputs(t *testing.T) {
	base := []Source{NewHTTPSource("https://example.org/a.pdf")}
	baseOpts := &Options{ToFormats: []string{"md"}}
	baseKey := CacheKey(base, baseOpts)

	cases := []struct {
		name string
		src  []Source
		opts *Options
	}{
		{"different url", []Source{NewHTTPSource("https://example.org/b.pdf")}, baseOpts},
		{"different to_formats", base, &Options{ToFormats: []string{"json"}}},
		{"extra to_format", base, &Options{ToFormats: []string{"md", "json"}}},
		{"toggle ocr", base, &Options{ToFormats: []string{"md"}, DoOCR: Ptr(true)}},
		{"different page range", base, &Options{ToFormats: []string{"md"}, PageRange: []int{1, 5}}},
		{"nil opts", base, nil},
	}
	seen := map[string]string{baseKey: "base"}
	for _, tc := range cases {
		k := CacheKey(tc.src, tc.opts)
		if k == baseKey {
			t.Errorf("%s: key collided with base", tc.name)
		}
		if other, ok := seen[k]; ok {
			t.Errorf("%s: key collided with %s", tc.name, other)
		}
		seen[k] = tc.name
	}
}

func TestHashFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.bin")
	if err := os.WriteFile(path, []byte("hello docling"), 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := HashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != 64 {
		t.Errorf("expected 64 hex chars, got %d", len(h))
	}
	h2, err := HashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if h != h2 {
		t.Errorf("HashFile not deterministic: %s vs %s", h, h2)
	}
}

func TestSourceForFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.pdf")
	if err := os.WriteFile(path, []byte("pdf-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := SourceForFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Kind != "file" {
		t.Errorf("Kind = %q, want file", s.Kind)
	}
	if s.Filename != "doc.pdf" {
		t.Errorf("Filename = %q, want doc.pdf", s.Filename)
	}
	if got := s.Base64String; len(got) != len("sha256:")+64 || got[:7] != "sha256:" {
		t.Errorf("Base64String = %q, want sha256:<64hex>", got)
	}
}

func TestFileCache_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	fc, err := NewFileCache(dir, "vtest")
	if err != nil {
		t.Fatal(err)
	}

	if _, ok, err := fc.Get("missing"); err != nil || ok {
		t.Errorf("Get(missing) = (_, %v, %v), want (false, nil)", ok, err)
	}

	want := &ConvertResponse{
		Status:         "success",
		ProcessingTime: 1.25,
		Document:       Document{Filename: "x.pdf", MDContent: "# title"},
	}
	if err := fc.Put("k1", want); err != nil {
		t.Fatal(err)
	}

	got, ok, err := fc.Get("k1")
	if err != nil || !ok {
		t.Fatalf("Get(k1) = (_, %v, %v), want (true, nil)", ok, err)
	}
	if got.Status != want.Status || got.Document.MDContent != want.Document.MDContent {
		t.Errorf("roundtrip mismatch: got %+v want %+v", got, want)
	}

	if _, err := os.Stat(filepath.Join(dir, "vtest", "k1.json.zst")); err != nil {
		t.Errorf("expected cache file on disk: %v", err)
	}
}

func TestFileCache_WriteVersionInfo(t *testing.T) {
	dir := t.TempDir()
	fc, err := NewFileCache(dir, "vtest")
	if err != nil {
		t.Fatal(err)
	}
	v := map[string]any{"docling": "1.2.3", "platform": "linux"}
	if err := fc.WriteVersionInfo(v); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "vtest", "_info.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(b) == 0 {
		t.Error("expected non-empty _info.json")
	}
}

func TestFileCache_ConcurrentPut(t *testing.T) {
	dir := t.TempDir()
	fc, err := NewFileCache(dir, "v")
	if err != nil {
		t.Fatal(err)
	}
	resp := &ConvertResponse{Status: "success", Document: Document{Filename: "x"}}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fc.Put("same", resp); err != nil {
				t.Errorf("put: %v", err)
			}
		}()
	}
	wg.Wait()

	got, ok, err := fc.Get("same")
	if err != nil || !ok || got.Status != "success" {
		t.Errorf("after concurrent Put: got=%v ok=%v err=%v", got, ok, err)
	}
}

func TestNewFileCache_EmptyRoot(t *testing.T) {
	if _, err := NewFileCache("", "v"); err == nil {
		t.Error("expected error for empty root")
	}
}

func TestVersionHash_Deterministic(t *testing.T) {
	v1 := map[string]any{"a": 1, "b": "x", "c": []int{1, 2}}
	v2 := map[string]any{"c": []int{1, 2}, "a": 1, "b": "x"}

	h1 := versionHash(v1)
	h2 := versionHash(v2)
	if h1 != h2 {
		t.Errorf("versionHash not order-independent: %s vs %s", h1, h2)
	}
	if len(h1) != 12 {
		t.Errorf("expected 12-char hash, got %d", len(h1))
	}

	v3 := map[string]any{"a": 2, "b": "x"}
	if versionHash(v3) == h1 {
		t.Error("versionHash collided across distinct inputs")
	}
}

func TestDefaultCacheDir(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/tmp/xdg")
	got, err := DefaultCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	want := "/tmp/xdg/doclingclient"
	if got != want {
		t.Errorf("DefaultCacheDir = %q, want %q", got, want)
	}
}
