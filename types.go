package doclingclient

import (
	"encoding/json"
	"fmt"
	"strings"
)

// OutputFormat enumerates the to_formats values whose content this client
// actually surfaces. The docling-serve OutputFormat enum also defines "yaml",
// "html_split_page", and "vtt", but ExportDocumentResponse carries no field
// for them, so this client does not list them.
type OutputFormat string

const (
	FormatMD      OutputFormat = "md"
	FormatJSON    OutputFormat = "json"
	FormatHTML    OutputFormat = "html"
	FormatText    OutputFormat = "text"
	FormatDoctags OutputFormat = "doctags"
)

// ParseOutputFormat validates s and returns it as a typed OutputFormat.
// Surrounding whitespace is trimmed.
func ParseOutputFormat(s string) (OutputFormat, error) {
	f := OutputFormat(strings.TrimSpace(s))
	switch f {
	case FormatMD, FormatJSON, FormatHTML, FormatText, FormatDoctags:
		return f, nil
	}
	return "", fmt.Errorf("invalid output format %q (want md, json, html, text, or doctags)", s)
}

// ImageExportMode controls how images extracted from a document are placed in
// the output. Relevant for image-capable outputs (json, html, md). The empty
// value means "use server default".
type ImageExportMode string

const (
	ImageExportPlaceholder ImageExportMode = "placeholder"
	ImageExportEmbedded    ImageExportMode = "embedded"
	ImageExportReferenced  ImageExportMode = "referenced"
)

// ParseImageExportMode validates s. An empty string is accepted and returned
// as the zero value, meaning "use server default".
func ParseImageExportMode(s string) (ImageExportMode, error) {
	if s == "" {
		return "", nil
	}
	m := ImageExportMode(s)
	switch m {
	case ImageExportPlaceholder, ImageExportEmbedded, ImageExportReferenced:
		return m, nil
	}
	return "", fmt.Errorf("invalid image export mode %q (want placeholder, embedded, or referenced)", s)
}

// Pipeline selects the docling processing pipeline.
type Pipeline string

const (
	PipelineLegacy   Pipeline = "legacy"
	PipelineStandard Pipeline = "standard"
	PipelineVLM      Pipeline = "vlm"
	PipelineASR      Pipeline = "asr"
)

// ParsePipeline validates s. An empty string is accepted and returned as the
// zero value, meaning "use server default".
func ParsePipeline(s string) (Pipeline, error) {
	if s == "" {
		return "", nil
	}
	p := Pipeline(s)
	switch p {
	case PipelineLegacy, PipelineStandard, PipelineVLM, PipelineASR:
		return p, nil
	}
	return "", fmt.Errorf("invalid pipeline %q (want legacy, standard, vlm, or asr)", s)
}

// PDFBackend selects the PDF parsing backend used by docling-serve.
type PDFBackend string

const (
	PDFBackendPyPDFium2    PDFBackend = "pypdfium2"
	PDFBackendDoclingParse PDFBackend = "docling_parse"
	PDFBackendDLParseV1    PDFBackend = "dlparse_v1"
	PDFBackendDLParseV2    PDFBackend = "dlparse_v2"
	PDFBackendDLParseV4    PDFBackend = "dlparse_v4"
)

// ParsePDFBackend validates s. An empty string is accepted and returned as the
// zero value, meaning "use server default".
func ParsePDFBackend(s string) (PDFBackend, error) {
	if s == "" {
		return "", nil
	}
	b := PDFBackend(s)
	switch b {
	case PDFBackendPyPDFium2, PDFBackendDoclingParse, PDFBackendDLParseV1, PDFBackendDLParseV2, PDFBackendDLParseV4:
		return b, nil
	}
	return "", fmt.Errorf("invalid pdf backend %q (want pypdfium2, docling_parse, dlparse_v1, dlparse_v2, or dlparse_v4)", s)
}

// TableMode selects the table-structure extraction mode.
type TableMode string

const (
	TableModeFast     TableMode = "fast"
	TableModeAccurate TableMode = "accurate"
)

// ParseTableMode validates s. An empty string is accepted and returned as the
// zero value, meaning "use server default".
func ParseTableMode(s string) (TableMode, error) {
	if s == "" {
		return "", nil
	}
	m := TableMode(s)
	switch m {
	case TableModeFast, TableModeAccurate:
		return m, nil
	}
	return "", fmt.Errorf("invalid table mode %q (want fast or accurate)", s)
}

// ConversionStatus values reported by docling-serve on ConvertResponse.Status.
type ConversionStatus string

const (
	StatusPending        ConversionStatus = "pending"
	StatusStarted        ConversionStatus = "started"
	StatusFailure        ConversionStatus = "failure"
	StatusSuccess        ConversionStatus = "success"
	StatusPartialSuccess ConversionStatus = "partial_success"
	StatusSkipped        ConversionStatus = "skipped"
)

// SourceKind discriminates the concrete Source variants the server accepts.
type SourceKind string

const (
	SourceKindHTTP SourceKind = "http"
	SourceKindFile SourceKind = "file"
	SourceKindS3   SourceKind = "s3"
)

// Source describes one input document. Implementations are HTTPSource,
// FileSource, and S3Source; each marshals itself with a "kind" discriminator
// so the docling-serve schema's polymorphic input is preserved on the wire.
type Source interface {
	Kind() SourceKind
}

// HTTPSource fetches the document from a URL.
type HTTPSource struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

// Kind reports the source variant.
func (s HTTPSource) Kind() SourceKind { return SourceKindHTTP }

// MarshalJSON injects the "kind" discriminator alongside the type's fields.
func (s HTTPSource) MarshalJSON() ([]byte, error) {
	type Alias HTTPSource
	return json.Marshal(struct {
		Kind SourceKind `json:"kind"`
		Alias
	}{Kind: SourceKindHTTP, Alias: Alias(s)})
}

// FileSource carries a base64-encoded document body inline.
type FileSource struct {
	Base64String string `json:"base64_string"`
	Filename     string `json:"filename"`
}

// Kind reports the source variant.
func (s FileSource) Kind() SourceKind { return SourceKindFile }

// MarshalJSON injects the "kind" discriminator alongside the type's fields.
func (s FileSource) MarshalJSON() ([]byte, error) {
	type Alias FileSource
	return json.Marshal(struct {
		Kind SourceKind `json:"kind"`
		Alias
	}{Kind: SourceKindFile, Alias: Alias(s)})
}

// S3Source points the server at an S3 bucket prefix.
type S3Source struct {
	Endpoint  string `json:"endpoint"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
	Bucket    string `json:"bucket"`
	KeyPrefix string `json:"key_prefix,omitempty"`
	VerifySSL *bool  `json:"verify_ssl,omitempty"`
}

// Kind reports the source variant.
func (s S3Source) Kind() SourceKind { return SourceKindS3 }

// MarshalJSON injects the "kind" discriminator alongside the type's fields.
func (s S3Source) MarshalJSON() ([]byte, error) {
	type Alias S3Source
	return json.Marshal(struct {
		Kind SourceKind `json:"kind"`
		Alias
	}{Kind: SourceKindS3, Alias: Alias(s)})
}

// NewHTTPSource builds an HTTPSource for a remote URL.
func NewHTTPSource(url string) HTTPSource {
	return HTTPSource{URL: url}
}

// NewFileSource builds a FileSource for an in-body base64 upload.
func NewFileSource(filename, base64String string) FileSource {
	return FileSource{Filename: filename, Base64String: base64String}
}

// Options is a subset of ConvertDocumentsOptions covering the parameters most
// callers want to tweak. Zero values are omitted from the request, so the
// server uses its defaults.
//
// Pointer fields distinguish "unset" from "explicitly false/zero".
type Options struct {
	FromFormats      []string        `json:"from_formats,omitempty"`
	ToFormats        []OutputFormat  `json:"to_formats,omitempty"`
	ImageExportMode  ImageExportMode `json:"image_export_mode,omitempty"`
	DoOCR            *bool           `json:"do_ocr,omitempty"`
	ForceOCR         *bool           `json:"force_ocr,omitempty"`
	OCREngine        string          `json:"ocr_engine,omitempty"`
	OCRLang          []string        `json:"ocr_lang,omitempty"`
	OCRPreset        string          `json:"ocr_preset,omitempty"`
	PDFBackend       PDFBackend      `json:"pdf_backend,omitempty"`
	TableMode        TableMode       `json:"table_mode,omitempty"`
	Pipeline         Pipeline        `json:"pipeline,omitempty"`
	PageRange        []int           `json:"page_range,omitempty"`
	AbortOnError     *bool           `json:"abort_on_error,omitempty"`
	DoTableStructure *bool           `json:"do_table_structure,omitempty"`
	IncludeImages    *bool           `json:"include_images,omitempty"`
	ImagesScale      *float64        `json:"images_scale,omitempty"`
	DocumentTimeout  *float64        `json:"document_timeout,omitempty"`
}

// convertRequest is the JSON payload for /v1/convert/source.
type convertRequest struct {
	Options *Options `json:"options,omitempty"`
	Sources []Source `json:"sources"`
	Target  Target   `json:"target,omitempty"`
}

// TargetKind discriminates the concrete Target variants the server accepts on
// the /v1/convert/source endpoint.
type TargetKind string

const (
	TargetKindInBody TargetKind = "inbody"
	TargetKindPut    TargetKind = "put"
	TargetKindS3     TargetKind = "s3"
	TargetKindZip    TargetKind = "zip"
)

// Target selects where the conversion result is delivered. Implementations:
// InBodyTarget (default, document inline in response), ZipTarget (response is
// a zip), PutTarget (server PUTs the result to a URL), and S3Target (server
// uploads to S3). Each marshals itself with a "kind" discriminator.
type Target interface {
	Kind() TargetKind
}

// InBodyTarget asks the server to embed the converted document in the JSON
// response body. This is the server's default.
type InBodyTarget struct{}

// Kind reports the target variant.
func (t InBodyTarget) Kind() TargetKind { return TargetKindInBody }

// MarshalJSON injects the "kind" discriminator.
func (t InBodyTarget) MarshalJSON() ([]byte, error) {
	return []byte(`{"kind":"inbody"}`), nil
}

// ZipTarget asks the server to return the converted document as a zip blob.
type ZipTarget struct{}

// Kind reports the target variant.
func (t ZipTarget) Kind() TargetKind { return TargetKindZip }

// MarshalJSON injects the "kind" discriminator.
func (t ZipTarget) MarshalJSON() ([]byte, error) {
	return []byte(`{"kind":"zip"}`), nil
}

// PutTarget asks the server to HTTP PUT the converted result to URL.
type PutTarget struct {
	URL string `json:"url"`
}

// Kind reports the target variant.
func (t PutTarget) Kind() TargetKind { return TargetKindPut }

// MarshalJSON injects the "kind" discriminator alongside the type's fields.
func (t PutTarget) MarshalJSON() ([]byte, error) {
	type Alias PutTarget
	return json.Marshal(struct {
		Kind TargetKind `json:"kind"`
		Alias
	}{Kind: TargetKindPut, Alias: Alias(t)})
}

// S3Target asks the server to upload the converted result to S3.
type S3Target struct {
	Endpoint  string `json:"endpoint"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
	Bucket    string `json:"bucket"`
	KeyPrefix string `json:"key_prefix,omitempty"`
	VerifySSL *bool  `json:"verify_ssl,omitempty"`
}

// Kind reports the target variant.
func (t S3Target) Kind() TargetKind { return TargetKindS3 }

// MarshalJSON injects the "kind" discriminator alongside the type's fields.
func (t S3Target) MarshalJSON() ([]byte, error) {
	type Alias S3Target
	return json.Marshal(struct {
		Kind TargetKind `json:"kind"`
		Alias
	}{Kind: TargetKindS3, Alias: Alias(t)})
}

// TargetType is the value the multipart /v1/convert/file endpoint accepts in
// its "target_type" form field. The file endpoint does not support PutTarget
// or S3Target — only inbody or zip.
type TargetType string

const (
	TargetTypeInBody TargetType = "inbody"
	TargetTypeZip    TargetType = "zip"
)

// ConvertResponse is the synchronous response from /v1/convert/{source,file}
// when a single in-body document is requested.
type ConvertResponse struct {
	Document       Document         `json:"document"`
	Status         ConversionStatus `json:"status"`
	Errors         []ErrorItem      `json:"errors,omitempty"`
	ProcessingTime float64          `json:"processing_time"`
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

// ConvertError is returned by ConvertResponse.Err when the server reports a
// non-success status. It carries the conversion status and any per-component
// errors the server attached.
type ConvertError struct {
	Status ConversionStatus
	Errors []ErrorItem
}

func (e *ConvertError) Error() string {
	if len(e.Errors) == 0 {
		return fmt.Sprintf("docling: conversion %s with no error details", e.Status)
	}
	parts := make([]string, 0, len(e.Errors))
	for _, ei := range e.Errors {
		parts = append(parts, fmt.Sprintf("[%s/%s] %s", ei.ComponentType, ei.ModuleName, ei.ErrorMessage))
	}
	return fmt.Sprintf("docling: conversion %s: %s", e.Status, strings.Join(parts, "; "))
}

// Err returns a non-nil error when the server reports a failure status or
// when partialIsError is true and the status is partial_success. The HTTP
// call itself can succeed while the conversion fails — callers that don't
// check Status by hand should call this.
func (r *ConvertResponse) Err(partialIsError bool) error {
	switch r.Status {
	case StatusFailure:
		return &ConvertError{Status: r.Status, Errors: r.Errors}
	case StatusPartialSuccess:
		if partialIsError {
			return &ConvertError{Status: r.Status, Errors: r.Errors}
		}
	}
	return nil
}

// Ptr returns a pointer to v. Handy for Options fields like DoOCR.
func Ptr[T any](v T) *T { return &v }

// castStrings widens a slice of any underlying-string type to []string. Used
// internally to feed typed enum slices (e.g. []OutputFormat) into helpers
// that accept []string.
func castStrings[T ~string](vs []T) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = string(v)
	}
	return out
}
