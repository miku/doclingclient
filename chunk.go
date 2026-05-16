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
	"strconv"
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

type chunkRequest[T any] struct {
	Sources             []Source `json:"sources"`
	ConvertOptions      *Options `json:"convert_options,omitempty"`
	ChunkingOptions     *T       `json:"chunking_options,omitempty"`
	IncludeConvertedDoc *bool    `json:"include_converted_doc,omitempty"`
}

// ChunkHybrid sends a JSON request to /v1/chunk/hybrid/source.
func (c *Client) ChunkHybrid(ctx context.Context, sources []Source, convertOpts *Options, chunkOpts *HybridChunkerOptions) (*ChunkResponse, error) {
	return chunkJSON(ctx, c, "/v1/chunk/hybrid/source", sources, convertOpts, chunkOpts)
}

// ChunkHierarchical sends a JSON request to /v1/chunk/hierarchical/source.
func (c *Client) ChunkHierarchical(ctx context.Context, sources []Source, convertOpts *Options, chunkOpts *HierarchicalChunkerOptions) (*ChunkResponse, error) {
	return chunkJSON(ctx, c, "/v1/chunk/hierarchical/source", sources, convertOpts, chunkOpts)
}

// ChunkHybridFile uploads files via multipart/form-data to
// /v1/chunk/hybrid/file.
func (c *Client) ChunkHybridFile(ctx context.Context, files []FileUpload, convertOpts *Options, chunkOpts *HybridChunkerOptions) (*ChunkResponse, error) {
	return chunkFile(ctx, c, "/v1/chunk/hybrid/file", files, convertOpts, func(mw *multipart.Writer) error {
		return writeHybridChunkerOptionsForm(mw, chunkOpts)
	})
}

// ChunkHierarchicalFile uploads files via multipart/form-data to
// /v1/chunk/hierarchical/file.
func (c *Client) ChunkHierarchicalFile(ctx context.Context, files []FileUpload, convertOpts *Options, chunkOpts *HierarchicalChunkerOptions) (*ChunkResponse, error) {
	return chunkFile(ctx, c, "/v1/chunk/hierarchical/file", files, convertOpts, func(mw *multipart.Writer) error {
		return writeHierarchicalChunkerOptionsForm(mw, chunkOpts)
	})
}

func chunkJSON[T any](ctx context.Context, c *Client, path string, sources []Source, convertOpts *Options, chunkOpts *T) (*ChunkResponse, error) {
	if len(sources) == 0 {
		return nil, fmt.Errorf("doclingclient: no sources provided")
	}
	body := chunkRequest[T]{
		Sources:         sources,
		ConvertOptions:  convertOpts,
		ChunkingOptions: chunkOpts,
	}
	var out ChunkResponse
	if err := c.postJSON(ctx, path, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func chunkFile(ctx context.Context, c *Client, path string, files []FileUpload, convertOpts *Options, writeChunkOpts func(*multipart.Writer) error) (*ChunkResponse, error) {
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

		if convertOpts != nil {
			if err = writeOptionsForm(mw, convertOpts, "convert_"); err != nil {
				return
			}
		}
		if err = writeChunkOpts(mw); err != nil {
			return
		}
		for _, f := range files {
			var part io.Writer
			part, err = mw.CreateFormFile("files", f.Name)
			if err != nil {
				return
			}
			if _, err = io.Copy(part, f.Content); err != nil {
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

func writeHybridChunkerOptionsForm(mw *multipart.Writer, o *HybridChunkerOptions) error {
	if o == nil {
		return nil
	}
	if o.MaxTokens != nil {
		if err := mw.WriteField("chunking_max_tokens", strconv.Itoa(*o.MaxTokens)); err != nil {
			return err
		}
	}
	if o.Tokenizer != "" {
		if err := mw.WriteField("chunking_tokenizer", o.Tokenizer); err != nil {
			return err
		}
	}
	if o.MergePeers != nil {
		if err := mw.WriteField("chunking_merge_peers", strconv.FormatBool(*o.MergePeers)); err != nil {
			return err
		}
	}
	if o.UseMarkdownTables != nil {
		if err := mw.WriteField("chunking_use_markdown_tables", strconv.FormatBool(*o.UseMarkdownTables)); err != nil {
			return err
		}
	}
	if o.IncludeRawText != nil {
		if err := mw.WriteField("chunking_include_raw_text", strconv.FormatBool(*o.IncludeRawText)); err != nil {
			return err
		}
	}
	return nil
}

func writeHierarchicalChunkerOptionsForm(mw *multipart.Writer, o *HierarchicalChunkerOptions) error {
	if o == nil {
		return nil
	}
	if o.UseMarkdownTables != nil {
		if err := mw.WriteField("chunking_use_markdown_tables", strconv.FormatBool(*o.UseMarkdownTables)); err != nil {
			return err
		}
	}
	if o.IncludeRawText != nil {
		if err := mw.WriteField("chunking_include_raw_text", strconv.FormatBool(*o.IncludeRawText)); err != nil {
			return err
		}
	}
	return nil
}

// ChunkHybridPath is a convenience wrapper around ChunkHybridFile for a single
// local file path.
func (c *Client) ChunkHybridPath(ctx context.Context, path string, convertOpts *Options, chunkOpts *HybridChunkerOptions) (*ChunkResponse, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return c.ChunkHybridFile(ctx, []FileUpload{{Name: filepath.Base(path), Content: f}}, convertOpts, chunkOpts)
}

// ChunkHierarchicalPath is a convenience wrapper around ChunkHierarchicalFile
// for a single local file path.
func (c *Client) ChunkHierarchicalPath(ctx context.Context, path string, convertOpts *Options, chunkOpts *HierarchicalChunkerOptions) (*ChunkResponse, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return c.ChunkHierarchicalFile(ctx, []FileUpload{{Name: filepath.Base(path), Content: f}}, convertOpts, chunkOpts)
}
