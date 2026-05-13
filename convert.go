package doclingclient

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
)

// Convert sends a JSON request to /v1/convert/source. Use this when your
// inputs are URLs or already base64-encoded files.
func (c *Client) Convert(ctx context.Context, sources []Source, opts *Options) (*ConvertResponse, error) {
	if len(sources) == 0 {
		return nil, fmt.Errorf("doclingclient: no sources provided")
	}
	body := convertRequest{Options: opts, Sources: sources}
	var out ConvertResponse
	if err := c.postJSON(ctx, "/v1/convert/source", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ConvertURL is a convenience wrapper around Convert for a single HTTP source.
func (c *Client) ConvertURL(ctx context.Context, url string, opts *Options) (*ConvertResponse, error) {
	return c.Convert(ctx, []Source{NewHTTPSource(url)}, opts)
}

// FileUpload describes a single file to upload to /v1/convert/file.
type FileUpload struct {
	// Name is the filename reported to the server (e.g. "paper.pdf").
	Name string
	// Content is the file body. It is read once; the caller retains ownership.
	Content io.Reader
}

// ConvertFile uploads files via multipart/form-data to /v1/convert/file.
// Prefer this over Convert for large local files to avoid base64 overhead.
func (c *Client) ConvertFile(ctx context.Context, files []FileUpload, opts *Options) (*ConvertResponse, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("doclingclient: no files provided")
	}

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)

	go func() {
		// Errors from the goroutine are surfaced via CloseWithError on the
		// pipe; the http transport will then see them as a read error.
		var err error
		defer func() {
			cerr := mw.Close()
			if err == nil {
				err = cerr
			}
			_ = pw.CloseWithError(err)
		}()

		if opts != nil {
			if err = writeOptionsForm(mw, opts); err != nil {
				return
			}
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

	req, err := c.newRequest(ctx, http.MethodPost, "/v1/convert/file", pr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	var out ConvertResponse
	if err := c.doJSON(req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ConvertReader is a convenience wrapper for converting a single file from an
// io.Reader.
func (c *Client) ConvertReader(ctx context.Context, r io.Reader, filename string, opts *Options) (*ConvertResponse, error) {
	return c.ConvertFile(ctx, []FileUpload{{Name: filename, Content: r}}, opts)
}

// ConvertPath opens a local file and uploads it via /v1/convert/file.
func (c *Client) ConvertPath(ctx context.Context, path string, opts *Options) (*ConvertResponse, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return c.ConvertReader(ctx, f, filepath.Base(path), opts)
}

// EncodeFile reads path and returns a base64-encoded Source suitable for
// Convert. Most callers should prefer ConvertPath, which streams the upload.
func EncodeFile(path string) (Source, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Source{}, err
	}
	return NewFileSource(filepath.Base(path), base64.StdEncoding.EncodeToString(b)), nil
}

// writeOptionsForm writes Options fields as multipart form fields. List-typed
// fields are written as repeated values, matching what the FastAPI form
// parser expects.
func writeOptionsForm(mw *multipart.Writer, o *Options) error {
	write := func(k, v string) error { return mw.WriteField(k, v) }
	writeList := func(k string, vs []string) error {
		for _, v := range vs {
			if err := mw.WriteField(k, v); err != nil {
				return err
			}
		}
		return nil
	}
	writeBool := func(k string, b *bool) error {
		if b == nil {
			return nil
		}
		return write(k, strconv.FormatBool(*b))
	}
	writeFloat := func(k string, f *float64) error {
		if f == nil {
			return nil
		}
		return write(k, strconv.FormatFloat(*f, 'f', -1, 64))
	}

	if err := writeList("from_formats", o.FromFormats); err != nil {
		return err
	}
	if err := writeList("to_formats", o.ToFormats); err != nil {
		return err
	}
	if o.ImageExportMode != "" {
		if err := write("image_export_mode", o.ImageExportMode); err != nil {
			return err
		}
	}
	if err := writeBool("do_ocr", o.DoOCR); err != nil {
		return err
	}
	if err := writeBool("force_ocr", o.ForceOCR); err != nil {
		return err
	}
	if o.OCREngine != "" {
		if err := write("ocr_engine", o.OCREngine); err != nil {
			return err
		}
	}
	if err := writeList("ocr_lang", o.OCRLang); err != nil {
		return err
	}
	if o.OCRPreset != "" {
		if err := write("ocr_preset", o.OCRPreset); err != nil {
			return err
		}
	}
	if o.PDFBackend != "" {
		if err := write("pdf_backend", o.PDFBackend); err != nil {
			return err
		}
	}
	if o.TableMode != "" {
		if err := write("table_mode", o.TableMode); err != nil {
			return err
		}
	}
	if o.Pipeline != "" {
		if err := write("pipeline", o.Pipeline); err != nil {
			return err
		}
	}
	for _, p := range o.PageRange {
		if err := write("page_range", strconv.Itoa(p)); err != nil {
			return err
		}
	}
	if err := writeBool("abort_on_error", o.AbortOnError); err != nil {
		return err
	}
	if err := writeBool("do_table_structure", o.DoTableStructure); err != nil {
		return err
	}
	if err := writeBool("include_images", o.IncludeImages); err != nil {
		return err
	}
	if err := writeFloat("images_scale", o.ImagesScale); err != nil {
		return err
	}
	if err := writeFloat("document_timeout", o.DocumentTimeout); err != nil {
		return err
	}
	return nil
}
