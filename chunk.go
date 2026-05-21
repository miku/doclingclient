package doclingclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
)

// Chunker identifies the chunking strategy. The two variants map to distinct
// docling-serve endpoint paths.
type Chunker string

const (
	ChunkerHybrid       Chunker = "hybrid"
	ChunkerHierarchical Chunker = "hierarchical"
)

// HybridChunkerOptions configures the HybridChunker, which produces
// tokenization-aware chunks on top of hierarchical chunking. See docling's
// concepts/chunking docs for the algorithm.
type HybridChunkerOptions struct {
	// MaxTokens caps the per-chunk token count. When nil the server derives
	// it from the tokenizer.
	MaxTokens *int `json:"max_tokens,omitempty"`
	// Tokenizer is a HuggingFace model name. Server default is
	// "sentence-transformers/all-MiniLM-L6-v2".
	Tokenizer string `json:"tokenizer,omitempty"`
	// MergePeers merges undersized successive chunks sharing the same
	// headings. Server default true.
	MergePeers *bool `json:"merge_peers,omitempty"`
	// UseMarkdownTables serializes tables as Markdown instead of triplets.
	UseMarkdownTables *bool `json:"use_markdown_tables,omitempty"`
	// IncludeRawText asks the server to populate Chunk.RawText alongside Text.
	IncludeRawText *bool `json:"include_raw_text,omitempty"`
}

// HierarchicalChunkerOptions configures the HierarchicalChunker, which yields
// one chunk per detected document element with no tokenization awareness.
type HierarchicalChunkerOptions struct {
	UseMarkdownTables *bool `json:"use_markdown_tables,omitempty"`
	IncludeRawText    *bool `json:"include_raw_text,omitempty"`
}

// Chunk is one piece of a chunked document.
type Chunk struct {
	Filename    string         `json:"filename"`
	ChunkIndex  int            `json:"chunk_index"`
	Text        string         `json:"text"`
	RawText     string         `json:"raw_text,omitempty"`
	NumTokens   *int           `json:"num_tokens,omitempty"`
	Headings    []string       `json:"headings,omitempty"`
	Captions    []string       `json:"captions,omitempty"`
	DocItems    []string       `json:"doc_items"`
	PageNumbers []int          `json:"page_numbers,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// ChunkResponse is the synchronous response from /v1/chunk/{...}/{source,file}.
// Documents is populated only when include_converted_doc was requested; its
// elements follow the server's ExportResult schema, which this client does
// not type yet, so they are exposed as raw JSON.
type ChunkResponse struct {
	Chunks         []Chunk           `json:"chunks"`
	Documents      []json.RawMessage `json:"documents,omitempty"`
	ProcessingTime float64           `json:"processing_time"`
}

// ChunkHybridURLRequest is the body of POST /v1/chunk/hybrid/source.
type ChunkHybridURLRequest struct {
	Sources             []Source             `json:"sources"`
	ConvertOptions      ConvertOptions       `json:"convert_options,omitzero"`
	ChunkingOptions     HybridChunkerOptions `json:"chunking_options,omitzero"`
	IncludeConvertedDoc bool                 `json:"include_converted_doc,omitempty"`
}

// ChunkHierarchicalURLRequest is the body of POST /v1/chunk/hierarchical/source.
type ChunkHierarchicalURLRequest struct {
	Sources             []Source                   `json:"sources"`
	ConvertOptions      ConvertOptions             `json:"convert_options,omitzero"`
	ChunkingOptions     HierarchicalChunkerOptions `json:"chunking_options,omitzero"`
	IncludeConvertedDoc bool                       `json:"include_converted_doc,omitempty"`
}

// ChunkHybridFileRequest bundles the multipart inputs to
// POST /v1/chunk/hybrid/file.
type ChunkHybridFileRequest struct {
	Files               []File
	ConvertOptions      ConvertOptions
	ChunkingOptions     HybridChunkerOptions
	IncludeConvertedDoc bool
}

// ChunkHierarchicalFileRequest bundles the multipart inputs to
// POST /v1/chunk/hierarchical/file.
type ChunkHierarchicalFileRequest struct {
	Files               []File
	ConvertOptions      ConvertOptions
	ChunkingOptions     HierarchicalChunkerOptions
	IncludeConvertedDoc bool
}

// ChunkHybridURL sends a JSON request to /v1/chunk/hybrid/source.
func (c *Client) ChunkHybridURL(ctx context.Context, req ChunkHybridURLRequest) (*ChunkResponse, error) {
	if len(req.Sources) == 0 {
		return nil, fmt.Errorf("doclingclient: no sources provided")
	}
	var out ChunkResponse
	if err := c.postJSON(ctx, "/v1/chunk/hybrid/source", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ChunkHierarchicalURL sends a JSON request to /v1/chunk/hierarchical/source.
func (c *Client) ChunkHierarchicalURL(ctx context.Context, req ChunkHierarchicalURLRequest) (*ChunkResponse, error) {
	if len(req.Sources) == 0 {
		return nil, fmt.Errorf("doclingclient: no sources provided")
	}
	var out ChunkResponse
	if err := c.postJSON(ctx, "/v1/chunk/hierarchical/source", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ChunkHybridFile uploads files via multipart/form-data to
// /v1/chunk/hybrid/file.
func (c *Client) ChunkHybridFile(ctx context.Context, req ChunkHybridFileRequest) (*ChunkResponse, error) {
	return chunkFile(ctx, c, "/v1/chunk/hybrid/file", req.Files, req.ConvertOptions, req.ChunkingOptions, req.IncludeConvertedDoc)
}

// ChunkHierarchicalFile uploads files via multipart/form-data to
// /v1/chunk/hierarchical/file.
func (c *Client) ChunkHierarchicalFile(ctx context.Context, req ChunkHierarchicalFileRequest) (*ChunkResponse, error) {
	return chunkFile(ctx, c, "/v1/chunk/hierarchical/file", req.Files, req.ConvertOptions, req.ChunkingOptions, req.IncludeConvertedDoc)
}

func chunkFile(ctx context.Context, c *Client, path string, files []File, convertOpts ConvertOptions, chunkOpts any, includeConvertedDoc bool) (*ChunkResponse, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("doclingclient: no files provided")
	}

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)

	go func() {
		var err error
		defer func() {
			cerr := mw.Close()
			if err == nil {
				err = cerr
			}
			_ = pw.CloseWithError(err)
		}()

		if err = encodeFormFields(mw, convertOpts, "convert_"); err != nil {
			return
		}
		if err = encodeFormFields(mw, chunkOpts, "chunking_"); err != nil {
			return
		}
		if includeConvertedDoc {
			if err = mw.WriteField("include_converted_doc", "true"); err != nil {
				return
			}
		}
		for _, f := range files {
			var part io.Writer
			part, err = mw.CreateFormFile("files", filepath.Base(f.Name()))
			if err != nil {
				return
			}
			if _, err = io.Copy(part, f); err != nil {
				return
			}
		}
	}()

	req, err := c.newRequest(ctx, http.MethodPost, path, pr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	var out ChunkResponse
	if err := c.doJSON(req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ChunkHybridPath is a convenience wrapper around ChunkHybridFile for a single
// local file path.
func (c *Client) ChunkHybridPath(ctx context.Context, path string, convertOpts ConvertOptions, chunkOpts HybridChunkerOptions) (*ChunkResponse, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return c.ChunkHybridFile(ctx, ChunkHybridFileRequest{
		Files:           []File{FileReader{Filename: filepath.Base(path), Reader: f}},
		ConvertOptions:  convertOpts,
		ChunkingOptions: chunkOpts,
	})
}

// ChunkHierarchicalPath is a convenience wrapper around ChunkHierarchicalFile
// for a single local file path.
func (c *Client) ChunkHierarchicalPath(ctx context.Context, path string, convertOpts ConvertOptions, chunkOpts HierarchicalChunkerOptions) (*ChunkResponse, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return c.ChunkHierarchicalFile(ctx, ChunkHierarchicalFileRequest{
		Files:           []File{FileReader{Filename: filepath.Base(path), Reader: f}},
		ConvertOptions:  convertOpts,
		ChunkingOptions: chunkOpts,
	})
}
