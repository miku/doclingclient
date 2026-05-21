package doclingclient

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChunkHybrid_JSONSource(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chunk/hybrid/source" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type = %q", ct)
		}
		var got struct {
			Sources         []map[string]any      `json:"sources"`
			ConvertOptions  *Options              `json:"convert_options"`
			ChunkingOptions *HybridChunkerOptions `json:"chunking_options"`
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if len(got.Sources) != 1 || got.Sources[0]["kind"] != "http" || got.Sources[0]["url"] != "https://example.org/x.pdf" {
			t.Errorf("sources = %+v", got.Sources)
		}
		if got.ChunkingOptions == nil || got.ChunkingOptions.Tokenizer != "Qwen/Qwen3-Embedding-0.6B" {
			t.Errorf("chunking_options = %+v", got.ChunkingOptions)
		}
		if got.ChunkingOptions.MaxTokens == nil || *got.ChunkingOptions.MaxTokens != 512 {
			t.Errorf("max_tokens = %+v", got.ChunkingOptions.MaxTokens)
		}
		_, _ = w.Write([]byte(`{"processing_time":0.1,"chunks":[{"filename":"x.pdf","chunk_index":0,"text":"hello","doc_items":["#/texts/0"],"num_tokens":1},{"filename":"x.pdf","chunk_index":1,"text":"world","doc_items":["#/texts/1"],"num_tokens":1}]}`))
	}))
	defer srv.Close()

	resp, err := New(srv.URL).ChunkHybrid(
		context.Background(),
		[]Source{NewHTTPSource("https://example.org/x.pdf")},
		nil,
		&HybridChunkerOptions{
			MaxTokens: Ptr(512),
			Tokenizer: "Qwen/Qwen3-Embedding-0.6B",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Chunks) != 2 || resp.Chunks[1].Text != "world" {
		t.Errorf("chunks = %+v", resp.Chunks)
	}
	if resp.Chunks[0].NumTokens == nil || *resp.Chunks[0].NumTokens != 1 {
		t.Errorf("num_tokens not decoded: %+v", resp.Chunks[0])
	}
}

func TestChunkHierarchical_JSONSource(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chunk/hierarchical/source" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"processing_time":0.05,"chunks":[{"filename":"x.pdf","chunk_index":0,"text":"heading","doc_items":["#/texts/0"]}]}`))
	}))
	defer srv.Close()

	resp, err := New(srv.URL).ChunkHierarchical(
		context.Background(),
		[]Source{NewHTTPSource("https://example.org/x.pdf")},
		nil,
		&HierarchicalChunkerOptions{UseMarkdownTables: Ptr(true)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Chunks) != 1 {
		t.Errorf("chunks = %+v", resp.Chunks)
	}
}

func TestChunkHybridFile_Multipart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chunk/hybrid/file" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			t.Errorf("content-type = %q", r.Header.Get("Content-Type"))
		}
		mr, err := r.MultipartReader()
		if err != nil {
			t.Fatal(err)
		}
		var (
			sawFile        bool
			tokenizer      string
			maxTokens      string
			convertBackend string
		)
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case part.FileName() == "paper.pdf":
				sawFile = true
			case part.FormName() == "chunking_tokenizer":
				b, _ := io.ReadAll(part)
				tokenizer = string(b)
			case part.FormName() == "chunking_max_tokens":
				b, _ := io.ReadAll(part)
				maxTokens = string(b)
			case part.FormName() == "convert_pdf_backend":
				b, _ := io.ReadAll(part)
				convertBackend = string(b)
			}
		}
		if !sawFile {
			t.Error("missing file part")
		}
		if tokenizer != "sentence-transformers/all-MiniLM-L6-v2" {
			t.Errorf("chunking_tokenizer = %q", tokenizer)
		}
		if maxTokens != "256" {
			t.Errorf("chunking_max_tokens = %q", maxTokens)
		}
		if convertBackend != "dlparse_v4" {
			t.Errorf("convert_pdf_backend = %q (prefix not applied?)", convertBackend)
		}
		_, _ = w.Write([]byte(`{"processing_time":0.2,"chunks":[]}`))
	}))
	defer srv.Close()

	_, err := New(srv.URL).ChunkHybridFile(
		context.Background(),
		[]FileUpload{{Name: "paper.pdf", Content: bytes.NewReader([]byte("pdf"))}},
		&Options{PDFBackend: PDFBackendDLParseV4},
		&HybridChunkerOptions{
			MaxTokens: Ptr(256),
			Tokenizer: "sentence-transformers/all-MiniLM-L6-v2",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestChunkHybrid_NoSources(t *testing.T) {
	c := New("http://does-not-matter")
	if _, err := c.ChunkHybrid(context.Background(), nil, nil, nil); err == nil {
		t.Error("expected error")
	}
}

func TestChunkHybridFile_NoFiles(t *testing.T) {
	c := New("http://does-not-matter")
	if _, err := c.ChunkHybridFile(context.Background(), nil, nil, nil); err == nil {
		t.Error("expected error")
	}
}
