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

// ProcessURLRequest is the body of POST /v1/convert/source. Sources is
// required; Target defaults to inbody (server's choice when omitted), and
// ConvertOptions stays at server defaults when left zero.
type ProcessURLRequest struct {
	Sources        []Source       `json:"sources"`
	Target         Target         `json:"target,omitempty"`
	ConvertOptions ConvertOptions `json:"options,omitzero"`
}

// ProcessFileRequest bundles the inputs to POST /v1/convert/file (multipart).
// Files is required. TargetType defaults to inbody when empty; the file
// endpoint only supports inbody or zip. ConvertOptions stays at server
// defaults when left zero.
type ProcessFileRequest struct {
	Files          []File
	TargetType     TargetType
	ConvertOptions ConvertOptions
}

// ProcessURL sends a JSON request to /v1/convert/source. Use this when your
// inputs are URLs or already base64-encoded files.
func (c *Client) ProcessURL(ctx context.Context, req ProcessURLRequest) (*ConvertResponse, error) {
	if len(req.Sources) == 0 {
		return nil, fmt.Errorf("doclingclient: no sources provided")
	}
	var out ConvertResponse
	if err := c.postJSON(ctx, "/v1/convert/source", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ProcessFile uploads files via multipart/form-data to /v1/convert/file.
// Prefer this over ProcessURL for large local files to avoid base64 overhead.
func (c *Client) ProcessFile(ctx context.Context, req ProcessFileRequest) (*ConvertResponse, error) {
	if len(req.Files) == 0 {
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

		if err = encodeFormFields(mw, req.ConvertOptions, ""); err != nil {
			return
		}
		if req.TargetType != "" {
			if err = mw.WriteField("target_type", string(req.TargetType)); err != nil {
				return
			}
		}
		for _, f := range req.Files {
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

	r, err := c.newRequest(ctx, http.MethodPost, "/v1/convert/file", pr)
	if err != nil {
		return nil, err
	}
	r.Header.Set("Content-Type", mw.FormDataContentType())

	var out ConvertResponse
	if err := c.doJSON(r, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ConvertURL is a convenience wrapper around ProcessURL for a single HTTP
// source. Pass a zero ConvertOptions value to accept all server defaults.
func (c *Client) ConvertURL(ctx context.Context, url string, opts ConvertOptions) (*ConvertResponse, error) {
	return c.ProcessURL(ctx, ProcessURLRequest{
		Sources:        []Source{NewHTTPSource(url)},
		ConvertOptions: opts,
	})
}

// ConvertReader is a convenience wrapper for converting a single file from
// an io.Reader. The filename is reported to the server in the multipart
// upload.
func (c *Client) ConvertReader(ctx context.Context, r io.Reader, filename string, opts ConvertOptions) (*ConvertResponse, error) {
	return c.ProcessFile(ctx, ProcessFileRequest{
		Files:          []File{FileReader{Filename: filename, Reader: r}},
		ConvertOptions: opts,
	})
}

// ConvertPath opens a local file and uploads it via /v1/convert/file.
func (c *Client) ConvertPath(ctx context.Context, path string, opts ConvertOptions) (*ConvertResponse, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return c.ConvertReader(ctx, f, filepath.Base(path), opts)
}

// EncodeFile reads path and returns a base64-encoded FileSource suitable for
// ProcessURL. Most callers should prefer ConvertPath, which streams the
// upload.
func EncodeFile(path string) (FileSource, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return FileSource{}, err
	}
	return NewFileSource(filepath.Base(path), base64.StdEncoding.EncodeToString(b)), nil
}
