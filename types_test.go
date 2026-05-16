package doclingclient

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestParseOutputFormat(t *testing.T) {
	cases := []struct {
		in      string
		want    OutputFormat
		wantErr bool
	}{
		{"md", FormatMD, false},
		{"json", FormatJSON, false},
		{"html", FormatHTML, false},
		{"text", FormatText, false},
		{"doctags", FormatDoctags, false},
		{" md ", FormatMD, false},
		{"yaml", "", true},
		{"vtt", "", true},
		{"", "", true},
		{"MD", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseOutputFormat(tc.in)
			if (err != nil) != tc.wantErr {
				t.Errorf("err=%v, wantErr=%v", err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseImageExportMode(t *testing.T) {
	cases := []struct {
		in      string
		want    ImageExportMode
		wantErr bool
	}{
		{"", "", false},
		{"placeholder", ImageExportPlaceholder, false},
		{"embedded", ImageExportEmbedded, false},
		{"referenced", ImageExportReferenced, false},
		{"bogus", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseImageExportMode(tc.in)
			if (err != nil) != tc.wantErr {
				t.Errorf("err=%v, wantErr=%v", err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParsePipeline(t *testing.T) {
	for _, in := range []string{"", "legacy", "standard", "vlm", "asr"} {
		if _, err := ParsePipeline(in); err != nil {
			t.Errorf("%q: unexpected err %v", in, err)
		}
	}
	if _, err := ParsePipeline("bogus"); err == nil {
		t.Error("expected error for bogus pipeline")
	}
}

func TestParsePDFBackend(t *testing.T) {
	for _, in := range []string{"", "pypdfium2", "docling_parse", "dlparse_v1", "dlparse_v2", "dlparse_v4"} {
		if _, err := ParsePDFBackend(in); err != nil {
			t.Errorf("%q: unexpected err %v", in, err)
		}
	}
	if _, err := ParsePDFBackend("bogus"); err == nil {
		t.Error("expected error for bogus backend")
	}
}

func TestParseTableMode(t *testing.T) {
	for _, in := range []string{"", "fast", "accurate"} {
		if _, err := ParseTableMode(in); err != nil {
			t.Errorf("%q: unexpected err %v", in, err)
		}
	}
	if _, err := ParseTableMode("bogus"); err == nil {
		t.Error("expected error for bogus mode")
	}
}

func TestOptions_JSONMarshalUsesEnumStrings(t *testing.T) {
	opts := Options{
		ToFormats:       []OutputFormat{FormatMD, FormatJSON},
		ImageExportMode: ImageExportEmbedded,
		Pipeline:        PipelineStandard,
		PDFBackend:      PDFBackendDLParseV4,
		TableMode:       TableModeAccurate,
	}
	b, err := json.Marshal(opts)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if mode, _ := got["image_export_mode"].(string); mode != "embedded" {
		t.Errorf("image_export_mode = %v, want embedded", got["image_export_mode"])
	}
	if pl, _ := got["pipeline"].(string); pl != "standard" {
		t.Errorf("pipeline = %v, want standard", got["pipeline"])
	}
	if tm, _ := got["table_mode"].(string); tm != "accurate" {
		t.Errorf("table_mode = %v, want accurate", got["table_mode"])
	}
	tofs, _ := got["to_formats"].([]any)
	if len(tofs) != 2 || tofs[0] != "md" || tofs[1] != "json" {
		t.Errorf("to_formats = %v", tofs)
	}
}

func TestConvertResponse_Err(t *testing.T) {
	ok := &ConvertResponse{Status: StatusSuccess}
	if err := ok.Err(false); err != nil {
		t.Errorf("success: unexpected err %v", err)
	}
	if err := ok.Err(true); err != nil {
		t.Errorf("success (strict): unexpected err %v", err)
	}

	skipped := &ConvertResponse{Status: StatusSkipped}
	if err := skipped.Err(true); err != nil {
		t.Errorf("skipped should not error: %v", err)
	}

	partial := &ConvertResponse{
		Status: StatusPartialSuccess,
		Errors: []ErrorItem{{ComponentType: "pipeline", ModuleName: "ocr", ErrorMessage: "x"}},
	}
	if err := partial.Err(false); err != nil {
		t.Errorf("partial (lenient): unexpected err %v", err)
	}
	if err := partial.Err(true); err == nil {
		t.Error("partial (strict): expected err")
	}

	fail := &ConvertResponse{
		Status: StatusFailure,
		Errors: []ErrorItem{
			{ComponentType: "pipeline", ModuleName: "ocr", ErrorMessage: "boom"},
			{ComponentType: "io", ModuleName: "read", ErrorMessage: "eof"},
		},
	}
	err := fail.Err(false)
	if err == nil {
		t.Fatal("failure: expected err")
	}
	var ce *ConvertError
	if !errors.As(err, &ce) {
		t.Fatalf("wrong type: %T", err)
	}
	if ce.Status != StatusFailure || len(ce.Errors) != 2 {
		t.Errorf("ConvertError = %+v", ce)
	}
	msg := err.Error()
	if !strings.Contains(msg, "boom") || !strings.Contains(msg, "eof") {
		t.Errorf("missing details in %q", msg)
	}

	failNoDetails := &ConvertResponse{Status: StatusFailure}
	if !strings.Contains(failNoDetails.Err(false).Error(), "no error details") {
		t.Errorf("expected no-error-details message")
	}
}

func TestCastStrings(t *testing.T) {
	in := []OutputFormat{FormatMD, FormatJSON, FormatHTML}
	got := castStrings(in)
	want := []string{"md", "json", "html"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("got %v, want %v", got, want)
		}
	}
}
