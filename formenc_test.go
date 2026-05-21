package doclingclient

import (
	"bytes"
	"io"
	"mime/multipart"
	"sort"
	"testing"
)

// collect parses the buffer mw wrote into and returns name → values, with
// each list sorted so test assertions are order-independent.
func collectForm(t *testing.T, body []byte, boundary string) map[string][]string {
	t.Helper()
	mr := multipart.NewReader(bytes.NewReader(body), boundary)
	out := map[string][]string{}
	for {
		part, err := mr.NextPart()
		if err != nil {
			break
		}
		b, _ := io.ReadAll(part)
		out[part.FormName()] = append(out[part.FormName()], string(b))
	}
	for _, vs := range out {
		sort.Strings(vs)
	}
	return out
}

func TestEncodeFormFields_OptionsScalarsAndSlices(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	opts := &ConvertOptions{
		FromFormats:     []string{"pdf"},
		ToFormats:       []OutputFormat{FormatMD, FormatJSON},
		ImageExportMode: ImageExportEmbedded,
		DoOCR:           Ptr(true),
		ImagesScale:     Ptr(2.0),
		PageRange:       []int{1, 5, 9},
		PDFBackend:      PDFBackendDLParseV4,
	}
	if err := encodeFormFields(mw, opts, ""); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	got := collectForm(t, buf.Bytes(), mw.Boundary())
	wantPairs := map[string][]string{
		"from_formats":      {"pdf"},
		"to_formats":        {"json", "md"}, // sorted
		"image_export_mode": {"embedded"},
		"do_ocr":            {"true"},
		"images_scale":      {"2"},
		"page_range":        {"1", "5", "9"},
		"pdf_backend":       {"dlparse_v4"},
	}
	for k, want := range wantPairs {
		gotv := got[k]
		if len(gotv) != len(want) {
			t.Errorf("%s: got %v, want %v", k, gotv, want)
			continue
		}
		for i := range want {
			if gotv[i] != want[i] {
				t.Errorf("%s[%d]: got %q, want %q", k, i, gotv[i], want[i])
			}
		}
	}
	// Unset fields with omitempty must not appear.
	for _, k := range []string{"ocr_engine", "do_table_structure", "force_ocr"} {
		if _, ok := got[k]; ok {
			t.Errorf("%s should be omitted (was unset)", k)
		}
	}
}

func TestEncodeFormFields_Prefix(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := encodeFormFields(mw, &HybridChunkerOptions{
		MaxTokens: Ptr(128),
		Tokenizer: "tok",
	}, "chunking_"); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	got := collectForm(t, buf.Bytes(), mw.Boundary())
	if v := got["chunking_max_tokens"]; len(v) != 1 || v[0] != "128" {
		t.Errorf("chunking_max_tokens = %v", v)
	}
	if v := got["chunking_tokenizer"]; len(v) != 1 || v[0] != "tok" {
		t.Errorf("chunking_tokenizer = %v", v)
	}
	// Bare names must not appear when a prefix is set.
	if _, ok := got["max_tokens"]; ok {
		t.Errorf("bare max_tokens leaked")
	}
}

func TestEncodeFormFields_NilPointerNoop(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	var opts *ConvertOptions
	if err := encodeFormFields(mw, opts, ""); err != nil {
		t.Fatal(err)
	}
	_ = mw.Close()
}
