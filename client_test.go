package doclingclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClient_New_TrimsTrailingSlash(t *testing.T) {
	c := New("http://example.org:5001/")
	if c.BaseURL != "http://example.org:5001" {
		t.Errorf("BaseURL = %q, want trimmed", c.BaseURL)
	}
}

func TestClient_New_EmptyDefaults(t *testing.T) {
	c := New("")
	if c.BaseURL != DefaultBaseURL {
		t.Errorf("BaseURL = %q, want %q", c.BaseURL, DefaultBaseURL)
	}
	if c.HTTPClient == nil {
		t.Error("HTTPClient nil")
	}
}

func TestClient_New_WithOptions(t *testing.T) {
	custom := &http.Client{}
	c := New("http://example.org",
		WithAPIKey("sk-1"),
		WithTenantID("t-2"),
		WithUserAgent("ua-3"),
		WithHTTPClient(custom),
	)
	if c.APIKey != "sk-1" || c.TenantID != "t-2" || c.UserAgent != "ua-3" {
		t.Errorf("options not applied: %+v", c)
	}
	if c.HTTPClient != custom {
		t.Error("WithHTTPClient did not replace client")
	}
}

func TestClient_WithTimeout(t *testing.T) {
	c := New("", WithTimeout(7*time.Second))
	if c.HTTPClient.Timeout != 7*time.Second {
		t.Errorf("Timeout = %v", c.HTTPClient.Timeout)
	}
	// WithTimeout after WithHTTPClient mutates the supplied client.
	custom := &http.Client{}
	c2 := New("", WithHTTPClient(custom), WithTimeout(3*time.Second))
	if custom.Timeout != 3*time.Second {
		t.Errorf("custom.Timeout = %v", custom.Timeout)
	}
	_ = c2
}

func TestHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Errorf("path = %q, want /health", r.URL.Path)
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept header missing")
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	h, err := New(srv.URL).Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if h.Status != "ok" {
		t.Errorf("Status = %q, want ok", h.Status)
	}
}

func TestReady(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ready" {
			t.Errorf("path = %q, want /ready", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	}))
	defer srv.Close()

	h, err := New(srv.URL).Ready(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if h.Status != "ready" {
		t.Errorf("Status = %q, want ready", h.Status)
	}
}

func TestVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"docling":"1.2.3","platform":"linux"}`))
	}))
	defer srv.Close()

	v, err := New(srv.URL).Version(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if v["docling"] != "1.2.3" {
		t.Errorf("version = %v", v)
	}
}

func TestAPIErrorOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte("oops"))
	}))
	defer srv.Close()

	_, err := New(srv.URL).Health(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *APIError, got %T: %v", err, err)
	}
	if apiErr.Status != 500 || !strings.Contains(apiErr.Body, "oops") {
		t.Errorf("apiErr = %+v", apiErr)
	}
}

func TestAuthAndTenantHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "sk-test" {
			t.Errorf("X-Api-Key = %q", r.Header.Get("X-Api-Key"))
		}
		if r.Header.Get("X-Tenant-Id") != "tenant-7" {
			t.Errorf("X-Tenant-Id = %q", r.Header.Get("X-Tenant-Id"))
		}
		if r.Header.Get("User-Agent") != "doclingclient-go" {
			t.Errorf("User-Agent = %q", r.Header.Get("User-Agent"))
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.APIKey = "sk-test"
	c.TenantID = "tenant-7"
	if _, err := c.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestProcessURL_JSONSource(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/convert/source" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content-type = %q", r.Header.Get("Content-Type"))
		}
		var got struct {
			Sources []map[string]any `json:"sources"`
			Options *ConvertOptions  `json:"options"`
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if len(got.Sources) != 1 || got.Sources[0]["kind"] != "http" || got.Sources[0]["url"] != "https://example.org/x.pdf" {
			t.Errorf("sources = %+v", got.Sources)
		}
		if got.Options == nil || got.Options.ToFormats[0] != FormatMD {
			t.Errorf("options = %+v", got.Options)
		}
		_, _ = w.Write([]byte(`{"status":"success","processing_time":1.0,"document":{"filename":"x.pdf","md_content":"# hi"}}`))
	}))
	defer srv.Close()

	resp, err := New(srv.URL).ConvertURL(
		context.Background(),
		"https://example.org/x.pdf",
		ConvertOptions{ToFormats: []OutputFormat{FormatMD}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := resp.Document.MarkdownContent(); got != "# hi" {
		t.Errorf("md = %q", got)
	}
}

func TestProcessURL_PutTargetSerialized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got struct {
			Sources []map[string]any `json:"sources"`
			Target  map[string]any   `json:"target"`
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got.Target["kind"] != "put" {
			t.Errorf("target.kind = %v, want put", got.Target["kind"])
		}
		if got.Target["url"] != "https://sink.example/r" {
			t.Errorf("target.url = %v", got.Target["url"])
		}
		_, _ = w.Write([]byte(`{"status":"success","processing_time":0.1,"document":{"filename":"x.pdf"}}`))
	}))
	defer srv.Close()

	_, err := New(srv.URL).ProcessURL(
		context.Background(),
		ProcessURLRequest{
			Sources: []Source{NewHTTPSource("https://example.org/x.pdf")},
			Target:  PutTarget{URL: "https://sink.example/r"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestProcessFile_ZipTargetFormField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mr, err := r.MultipartReader()
		if err != nil {
			t.Fatal(err)
		}
		var targetType string
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if part.FormName() == "target_type" {
				b, _ := io.ReadAll(part)
				targetType = string(b)
			}
		}
		if targetType != "zip" {
			t.Errorf("target_type = %q, want zip", targetType)
		}
		_, _ = w.Write([]byte(`{"status":"success","processing_time":0,"document":{"filename":"x.pdf"}}`))
	}))
	defer srv.Close()

	_, err := New(srv.URL).ProcessFile(
		context.Background(),
		ProcessFileRequest{
			Files:      []File{FileReader{Filename: "x.pdf", Reader: bytes.NewReader([]byte("pdf"))}},
			TargetType: TargetTypeZip,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestProcessURL_NoSources(t *testing.T) {
	c := New("http://does-not-matter")
	if _, err := c.ProcessURL(context.Background(), ProcessURLRequest{}); err == nil {
		t.Error("expected error for empty sources")
	}
}

func TestProcessFile_Multipart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/convert/file" {
			t.Errorf("path = %q", r.URL.Path)
		}
		ct := r.Header.Get("Content-Type")
		if !strings.HasPrefix(ct, "multipart/form-data") {
			t.Errorf("content-type = %q", ct)
		}
		mr, err := r.MultipartReader()
		if err != nil {
			t.Fatal(err)
		}
		var sawFile bool
		var toFormats []string
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if part.FileName() == "paper.pdf" {
				sawFile = true
				continue
			}
			if part.FormName() == "to_formats" {
				b, _ := io.ReadAll(part)
				toFormats = append(toFormats, string(b))
			}
		}
		if !sawFile {
			t.Error("missing file part")
		}
		if len(toFormats) != 2 || toFormats[0] != "md" || toFormats[1] != "json" {
			t.Errorf("to_formats = %v", toFormats)
		}
		_, _ = w.Write([]byte(`{"status":"success","processing_time":0.5,"document":{"filename":"paper.pdf","md_content":"x"}}`))
	}))
	defer srv.Close()

	resp, err := New(srv.URL).ConvertReader(
		context.Background(),
		bytes.NewReader([]byte("fake-pdf-bytes")),
		"paper.pdf",
		ConvertOptions{ToFormats: []OutputFormat{FormatMD, FormatJSON}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != StatusSuccess {
		t.Errorf("status = %q", resp.Status)
	}
}

func TestProcessFile_NoFiles(t *testing.T) {
	c := New("http://does-not-matter")
	if _, err := c.ProcessFile(context.Background(), ProcessFileRequest{}); err == nil {
		t.Error("expected error for empty files")
	}
}

func TestConvert_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := New(srv.URL).Health(ctx)
	if err == nil {
		t.Error("expected error on cancelled context")
	}
}

func TestAPIError_TruncatesLongBody(t *testing.T) {
	body := strings.Repeat("x", 1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	_, err := New(srv.URL).Health(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	// Error message should contain the truncation marker.
	if !strings.HasSuffix(err.Error(), "...") {
		t.Errorf("expected truncation in error: %s", err.Error())
	}
}
