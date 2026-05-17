package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/miku/doclingclient"
)

func TestParseOutputFormats(t *testing.T) {
	cases := []struct {
		name    string
		in      []string
		wantErr bool
	}{
		{"single md", []string{"md"}, false},
		{"all valid", []string{"md", "json", "html", "text", "doctags"}, false},
		{"with whitespace", []string{" md ", "  json"}, false},
		{"unknown format", []string{"pdf"}, true},
		{"yaml is dropped", []string{"yaml"}, true},
		{"vtt is dropped", []string{"vtt"}, true},
		{"mixed valid + invalid", []string{"md", "bogus"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseOutputFormats(tc.in)
			if (err != nil) != tc.wantErr {
				t.Errorf("err=%v, wantErr=%v", err, tc.wantErr)
			}
			if err == nil && len(got) != len(tc.in) {
				t.Errorf("len(got)=%d, want %d", len(got), len(tc.in))
			}
		})
	}
}

func TestValidateStatusFormat(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"text", false},
		{"json", false},
		{"", true},
		{"JSON", true},
		{"yaml", true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			err := validateStatusFormat(tc.in)
			if (err != nil) != tc.wantErr {
				t.Errorf("err=%v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestSplitComma(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", []string{}},
		{"en", []string{"en"}},
		{"en,de", []string{"en", "de"}},
		{" en , de ", []string{"en", "de"}},
		{"en,,de", []string{"en", "de"}},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := splitComma(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestParsePageRange(t *testing.T) {
	cases := []struct {
		in      string
		want    []int
		wantErr bool
	}{
		{"3", []int{3, 3}, false},
		{"1-10", []int{1, 10}, false},
		{"abc", nil, true},
		{"-", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parsePageRange(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v, wantErr=%v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			if len(got) != len(tc.want) || got[0] != tc.want[0] || got[1] != tc.want[1] {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsURL(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"https://example.org/x.pdf", true},
		{"http://localhost:5001", true},
		{"./paper.pdf", false},
		{"/abs/paper.pdf", false},
		{"ftp://example.org/x", false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := isURL(tc.in); got != tc.want {
				t.Errorf("isURL(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestEnvOr(t *testing.T) {
	t.Setenv("FOO_X", "from-env")
	if got := envOr("FOO_X", "fallback"); got != "from-env" {
		t.Errorf("got %q, want from-env", got)
	}
	t.Setenv("FOO_X", "")
	if got := envOr("FOO_X", "fallback"); got != "fallback" {
		t.Errorf("got %q, want fallback", got)
	}
}

func TestWriteContent(t *testing.T) {
	doc := doclingclient.Document{
		Filename:       "x.pdf",
		MDContent:      "# hi",
		JSONContent:    json.RawMessage(`{"a":1}`),
		HTMLContent:    "<p>hi</p>",
		TextContent:    "hi",
		DoctagsContent: "<doc/>",
	}
	cases := []struct {
		format doclingclient.OutputFormat
		want   string
	}{
		{doclingclient.FormatMD, "# hi"},
		{doclingclient.FormatJSON, `{"a":1}`},
		{doclingclient.FormatHTML, "<p>hi</p>"},
		{doclingclient.FormatText, "hi"},
		{doclingclient.FormatDoctags, "<doc/>"},
	}
	for _, tc := range cases {
		t.Run(string(tc.format), func(t *testing.T) {
			var buf bytes.Buffer
			if err := writeContent(&buf, doc, tc.format); err != nil {
				t.Fatal(err)
			}
			if got := buf.String(); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWriteContent_EmptyContent(t *testing.T) {
	empty := doclingclient.Document{Filename: "x.pdf"}
	for _, f := range []doclingclient.OutputFormat{
		doclingclient.FormatMD, doclingclient.FormatJSON, doclingclient.FormatHTML,
		doclingclient.FormatText, doclingclient.FormatDoctags,
	} {
		t.Run(string(f), func(t *testing.T) {
			var buf bytes.Buffer
			if err := writeContent(&buf, empty, f); err == nil {
				t.Errorf("%s: expected error for empty content", f)
			}
		})
	}
}

func TestWriteContent_UnknownFormat(t *testing.T) {
	var buf bytes.Buffer
	err := writeContent(&buf, doclingclient.Document{MDContent: "x"}, doclingclient.OutputFormat("bogus"))
	if err == nil {
		t.Error("expected error for unknown format")
	}
}

func TestWriteStatus_Text(t *testing.T) {
	resp := &doclingclient.ConvertResponse{
		Status:         "success",
		ProcessingTime: 1.5,
		Document:       doclingclient.Document{Filename: "x.pdf"},
		Errors: []doclingclient.ErrorItem{
			{ComponentType: "pipeline", ModuleName: "ocr", ErrorMessage: "boom"},
		},
	}
	var buf bytes.Buffer
	if err := writeStatus(&buf, resp, true, "text"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "status=success") || !strings.Contains(out, "source=cached") {
		t.Errorf("missing fields in text status: %q", out)
	}
	if !strings.Contains(out, "[pipeline/ocr] boom") {
		t.Errorf("missing error line: %q", out)
	}
}

func TestFormatExtension(t *testing.T) {
	cases := map[doclingclient.OutputFormat]string{
		doclingclient.FormatMD:              ".md",
		doclingclient.FormatJSON:            ".json",
		doclingclient.FormatHTML:            ".html",
		doclingclient.FormatText:            ".txt",
		doclingclient.FormatDoctags:         ".doctags",
		doclingclient.OutputFormat("bogus"): "",
	}
	for in, want := range cases {
		t.Run(string(in), func(t *testing.T) {
			if got := formatExtension(in); got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

func TestWriteOutputs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "out")
	doc := doclingclient.Document{
		Filename:    "paper.pdf",
		MDContent:   "# hi",
		JSONContent: json.RawMessage(`{"a":1}`),
		HTMLContent: "<p>hi</p>",
	}
	if err := writeOutputs(dir, doc, []doclingclient.OutputFormat{doclingclient.FormatMD, doclingclient.FormatJSON, doclingclient.FormatHTML}); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"paper.md":   "# hi",
		"paper.json": `{"a":1}`,
		"paper.html": "<p>hi</p>",
	}
	for name, body := range want {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Errorf("missing %s: %v", name, err)
			continue
		}
		if string(got) != body {
			t.Errorf("%s: got %q, want %q", name, got, body)
		}
	}
}

func TestWriteOutputs_EmptyContent(t *testing.T) {
	dir := t.TempDir()
	doc := doclingclient.Document{Filename: "x.pdf"}
	if err := writeOutputs(dir, doc, []doclingclient.OutputFormat{doclingclient.FormatMD}); err == nil {
		t.Error("expected error for empty content")
	}
}

func TestWriteOutputs_FallbackBasename(t *testing.T) {
	dir := t.TempDir()
	doc := doclingclient.Document{MDContent: "# hi"}
	if err := writeOutputs(dir, doc, []doclingclient.OutputFormat{doclingclient.FormatMD}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "output.md")); err != nil {
		t.Errorf("expected output.md fallback: %v", err)
	}
}

func TestWriteStatus_JSON(t *testing.T) {
	resp := &doclingclient.ConvertResponse{
		Status:         "success",
		ProcessingTime: 1.5,
		Document:       doclingclient.Document{Filename: "x.pdf"},
	}
	var buf bytes.Buffer
	if err := writeStatus(&buf, resp, false, "json"); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Status         string                    `json:"status"`
		ProcessingTime float64                   `json:"processing_time"`
		Source         string                    `json:"source"`
		Filename       string                    `json:"filename"`
		Errors         []doclingclient.ErrorItem `json:"errors"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if got.Status != "success" || got.Source != "fresh" || got.Filename != "x.pdf" {
		t.Errorf("decoded mismatch: %+v", got)
	}
	if got.Errors == nil {
		t.Error("errors should be [] not null when empty")
	}
}
