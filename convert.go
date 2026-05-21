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
)

// Convert sends a JSON request to /v1/convert/source. Use this when your
// inputs are URLs or already base64-encoded files. The server defaults to
// inbody delivery; use ConvertWithTarget to request put/s3/zip.
func (c *Client) Convert(ctx context.Context, sources []Source, opts *Options) (*ConvertResponse, error) {
	return c.ConvertWithTarget(ctx, sources, opts, nil)
}

// ConvertWithTarget is Convert plus an explicit Target. A nil target leaves
// the server's default (inbody).
func (c *Client) ConvertWithTarget(ctx context.Context, sources []Source, opts *Options, target Target) (*ConvertResponse, error) {
	if len(sources) == 0 {
		return nil, fmt.Errorf("doclingclient: no sources provided")
	}
	body := convertRequest{Options: opts, Sources: sources, Target: target}
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
// The server defaults to inbody delivery; use ConvertFileWithTarget to
// request a zip response.
func (c *Client) ConvertFile(ctx context.Context, files []FileUpload, opts *Options) (*ConvertResponse, error) {
	return c.ConvertFileWithTarget(ctx, files, opts, "")
}

// ConvertFileWithTarget is ConvertFile plus an explicit target_type form
// field. The /v1/convert/file endpoint only supports inbody and zip; passing
// "" leaves the server's default (inbody).
func (c *Client) ConvertFileWithTarget(ctx context.Context, files []FileUpload, opts *Options, target TargetType) (*ConvertResponse, error) {
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
			if err = encodeFormFields(mw, opts, ""); err != nil {
				return
			}
		}
		if target != "" {
			if err = mw.WriteField("target_type", string(target)); err != nil {
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

// EncodeFile reads path and returns a base64-encoded FileSource suitable for
// Convert. Most callers should prefer ConvertPath, which streams the upload.
func EncodeFile(path string) (FileSource, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return FileSource{}, err
	}
	return NewFileSource(filepath.Base(path), base64.StdEncoding.EncodeToString(b)), nil
}

