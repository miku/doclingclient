package doclingclient

import "encoding/json"

// Output formats accepted by the to_formats option.
const (
	FormatMD      = "md"
	FormatJSON    = "json"
	FormatYAML    = "yaml"
	FormatHTML    = "html"
	FormatText    = "text"
	FormatDoctags = "doctags"
	FormatVTT     = "vtt"
)

// Image export modes accepted by the image_export_mode option. Only relevant
// for image-capable outputs (json, yaml, html, html_split_page, md).
const (
	ImageExportPlaceholder = "placeholder"
	ImageExportEmbedded    = "embedded"
	ImageExportReferenced  = "referenced"
)

// ConversionStatus values reported by docling-serve.
const (
	StatusPending        = "pending"
	StatusStarted        = "started"
	StatusFailure        = "failure"
	StatusSuccess        = "success"
	StatusPartialSuccess = "partial_success"
	StatusSkipped        = "skipped"
)

// Source describes one input document. Use NewHTTPSource, NewFileSource or
// build the struct manually.
type Source struct {
	// Kind is "http", "file" or "s3".
	Kind string `json:"kind"`

	// http
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`

	// file (base64-encoded inline upload)
	Base64String string `json:"base64_string,omitempty"`
	Filename     string `json:"filename,omitempty"`
}

// NewHTTPSource builds a Source for a remote URL.
func NewHTTPSource(url string) Source {
	return Source{Kind: "http", URL: url}
}

// NewFileSource builds a Source for an in-body base64 upload.
func NewFileSource(filename, base64String string) Source {
	return Source{Kind: "file", Filename: filename, Base64String: base64String}
}

// Options is a subset of ConvertDocumentsOptions covering the parameters most
// callers want to tweak. Zero values are omitted from the request, so the
// server uses its defaults.
//
// Pointer fields distinguish "unset" from "explicitly false/zero".
type Options struct {
	FromFormats      []string `json:"from_formats,omitempty"`
	ToFormats        []string `json:"to_formats,omitempty"`
	ImageExportMode  string   `json:"image_export_mode,omitempty"`
	DoOCR            *bool    `json:"do_ocr,omitempty"`
	ForceOCR         *bool    `json:"force_ocr,omitempty"`
	OCREngine        string   `json:"ocr_engine,omitempty"`
	OCRLang          []string `json:"ocr_lang,omitempty"`
	OCRPreset        string   `json:"ocr_preset,omitempty"`
	PDFBackend       string   `json:"pdf_backend,omitempty"`
	TableMode        string   `json:"table_mode,omitempty"`
	Pipeline         string   `json:"pipeline,omitempty"`
	PageRange        []int    `json:"page_range,omitempty"`
	AbortOnError     *bool    `json:"abort_on_error,omitempty"`
	DoTableStructure *bool    `json:"do_table_structure,omitempty"`
	IncludeImages    *bool    `json:"include_images,omitempty"`
	ImagesScale      *float64 `json:"images_scale,omitempty"`
	DocumentTimeout  *float64 `json:"document_timeout,omitempty"`
}

// convertRequest is the JSON payload for /v1/convert/source.
type convertRequest struct {
	Options *Options `json:"options,omitempty"`
	Sources []Source `json:"sources"`
}

// ConvertResponse is the synchronous response from /v1/convert/{source,file}
// when a single in-body document is requested.
type ConvertResponse struct {
	Document       Document    `json:"document"`
	Status         string      `json:"status"`
	Errors         []ErrorItem `json:"errors,omitempty"`
	ProcessingTime float64     `json:"processing_time"`
}

// Document holds the converted representations the server produced. Only the
// fields matching the requested to_formats are populated.
type Document struct {
	Filename       string          `json:"filename"`
	MDContent      string          `json:"md_content,omitempty"`
	JSONContent    json.RawMessage `json:"json_content,omitempty"`
	HTMLContent    string          `json:"html_content,omitempty"`
	TextContent    string          `json:"text_content,omitempty"`
	DoctagsContent string          `json:"doctags_content,omitempty"`
}

// ErrorItem is a per-component error reported by the converter.
type ErrorItem struct {
	ComponentType string `json:"component_type"`
	ModuleName    string `json:"module_name"`
	ErrorMessage  string `json:"error_message"`
}

// Ptr returns a pointer to v. Handy for Options fields like DoOCR.
func Ptr[T any](v T) *T { return &v }
