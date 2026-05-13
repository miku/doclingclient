// docli is a minimal CLI for a docling-serve instance.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/miku/doclingclient"
	"github.com/spf13/cobra"
)

// Version and Buildtime are injected at build time via -ldflags.
var (
	Version   = "dev"
	Buildtime = "unknown"
)

// Persistent flags shared by all subcommands.
type globalOpts struct {
	server   string
	apiKey   string
	tenantID string
}

func main() {
	root, _ := newRootCmd()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "docli: %v\n", err)
		os.Exit(1)
	}
}

func newRootCmd() (*cobra.Command, *globalOpts) {
	g := &globalOpts{}
	root := &cobra.Command{
		Use:           "docli",
		Short:         "Talk to a docling-serve instance",
		Long:          "docli is a small client CLI for a docling-serve document conversion service (https://github.com/docling-project/docling-serve).",
		Version:       fmt.Sprintf("%s (built %s)", Version, Buildtime),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVarP(&g.server, "server", "s", envOr("DOCLING_SERVER", doclingclient.DefaultBaseURL), "docling-serve base URL (env DOCLING_SERVER)")
	root.PersistentFlags().StringVarP(&g.apiKey, "api-key", "K", os.Getenv("DOCLING_API_KEY"), "API key, sent as X-Api-Key (env DOCLING_API_KEY)")
	root.PersistentFlags().StringVarP(&g.tenantID, "tenant", "T", os.Getenv("DOCLING_TENANT_ID"), "tenant ID, sent as X-Tenant-Id (env DOCLING_TENANT_ID)")

	root.AddCommand(newConvertCmd(g), newHealthCmd(g, "health"), newHealthCmd(g, "ready"), newVersionCmd(g))
	return root, g
}

func newClient(g *globalOpts) *doclingclient.Client {
	c := doclingclient.New(g.server)
	c.APIKey = g.apiKey
	c.TenantID = g.tenantID
	return c
}

func newConvertCmd(g *globalOpts) *cobra.Command {
	var (
		fromFormats     []string
		toFormats       []string
		ocr             bool
		forceOCR        bool
		ocrLang         string
		tableMode       string
		pages           string
		imageExportMode string
		abortOnError    bool
		docTimeout      float64
		doTables        bool
		imagesScale     float64
		includeImages   bool
		status          bool
		statusFormat    string
		cacheDir        string
		noCache         bool
	)
	cmd := &cobra.Command{
		Use:     "convert <url-or-path>",
		Short:   "Convert a document from a URL or local file",
		Args:    cobra.ExactArgs(1),
		Example: "  docli convert https://arxiv.org/pdf/2206.01062 > paper.md\n  docli convert --to json paper.pdf > paper.json\n  docli convert --to md,json paper.pdf > paper.md",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(toFormats) == 0 {
				return fmt.Errorf("--to requires at least one format")
			}
			if err := validateOutputFormats(toFormats); err != nil {
				return err
			}
			if err := validateImageExportMode(imageExportMode); err != nil {
				return err
			}
			if err := validateStatusFormat(statusFormat); err != nil {
				return err
			}
			primary := toFormats[0]

			opts := &doclingclient.Options{
				FromFormats:      fromFormats,
				ToFormats:        toFormats,
				DoOCR:            doclingclient.Ptr(ocr),
				ForceOCR:         doclingclient.Ptr(forceOCR),
				TableMode:        tableMode,
				ImageExportMode:  imageExportMode,
				AbortOnError:     doclingclient.Ptr(abortOnError),
				DoTableStructure: doclingclient.Ptr(doTables),
				IncludeImages:    doclingclient.Ptr(includeImages),
			}
			if ocrLang != "" {
				opts.OCRLang = splitComma(ocrLang)
			}
			if pages != "" {
				r, err := parsePageRange(pages)
				if err != nil {
					return err
				}
				opts.PageRange = r
			}
			if cmd.Flags().Changed("document-timeout") {
				opts.DocumentTimeout = doclingclient.Ptr(docTimeout)
			}
			if cmd.Flags().Changed("images-scale") {
				opts.ImagesScale = doclingclient.Ptr(imagesScale)
			}

			client := newClient(g)
			resp, cached, err := runConvertCached(cmd.Context(), client, args[0], opts, cacheDir, noCache)
			if err != nil {
				return err
			}
			if status {
				if err := writeStatus(os.Stderr, resp, cached, statusFormat); err != nil {
					return err
				}
			}
			return writeContent(cmd.OutOrStdout(), resp.Document, primary)
		},
	}
	cmd.Flags().StringSliceVar(&fromFormats, "from", nil, "input formats (e.g. pdf,docx); server autodetects if empty")
	cmd.Flags().StringSliceVarP(&toFormats, "to", "t", []string{"md"}, "output formats: md, json, yaml, html, text, doctags (first is written to stdout, all are cached)")
	cmd.Flags().BoolVar(&ocr, "ocr", true, "enable OCR")
	cmd.Flags().BoolVar(&forceOCR, "force-ocr", false, "force OCR over existing text")
	cmd.Flags().StringVar(&ocrLang, "ocr-lang", "", "comma-separated OCR languages, e.g. 'en,de'")
	cmd.Flags().StringVar(&tableMode, "table-mode", "", "table mode: fast or accurate (server default if empty)")
	cmd.Flags().StringVar(&pages, "pages", "", "page range, e.g. '1-10' or '3'")
	cmd.Flags().StringVar(&imageExportMode, "image-export-mode", "", "image export mode for image-capable outputs: placeholder, embedded, referenced (server default if empty)")
	cmd.Flags().BoolVar(&abortOnError, "abort-on-error", false, "abort the conversion on the first error")
	cmd.Flags().Float64Var(&docTimeout, "document-timeout", 0, "per-document timeout in seconds (server default if unset)")
	cmd.Flags().BoolVar(&doTables, "tables", true, "extract table structure")
	cmd.Flags().Float64Var(&imagesScale, "images-scale", 0, "scale factor for extracted images (server default 2.0 if unset)")
	cmd.Flags().BoolVar(&includeImages, "include-images", true, "include images extracted from the document")
	cmd.Flags().BoolVar(&status, "status", false, "print status and processing time to stderr")
	cmd.Flags().StringVar(&statusFormat, "status-format", "text", "format for --status output: text or json")
	cmd.Flags().StringVar(&cacheDir, "cache-dir", envOr("DOCLING_CACHE_DIR", ""), "cache directory (env DOCLING_CACHE_DIR, default XDG cache)")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "disable the on-disk result cache")
	return cmd
}

func newHealthCmd(g *globalOpts, name string) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: "Check the docling-serve /" + name + " endpoint",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient(g)
			var h *doclingclient.HealthResponse
			var err error
			if name == "ready" {
				h, err = c.Ready(cmd.Context())
			} else {
				h, err = c.Health(cmd.Context())
			}
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), h.Status)
			return nil
		},
	}
}

func newVersionCmd(g *globalOpts) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the docling-serve /version payload (use --version for the CLI version)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := newClient(g).Version(cmd.Context())
			if err != nil {
				return err
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(v)
		},
	}
}

func runConvert(ctx context.Context, c *doclingclient.Client, input string, opts *doclingclient.Options) (*doclingclient.ConvertResponse, error) {
	if isURL(input) {
		return c.ConvertURL(ctx, input, opts)
	}
	return c.ConvertPath(ctx, input, opts)
}

// runConvertCached wraps runConvert with an on-disk cache keyed by input
// fingerprint and namespaced by server version. Returns whether the result
// was served from cache.
//
// On any cache-related error (resolving cache dir, fetching server version,
// reading or writing the entry) it falls back to a live conversion rather
// than failing — the cache is an optimisation, not a source of truth.
func runConvertCached(ctx context.Context, c *doclingclient.Client, input string, opts *doclingclient.Options, cacheDir string, noCache bool) (*doclingclient.ConvertResponse, bool, error) {
	if noCache {
		resp, err := runConvert(ctx, c, input, opts)
		return resp, false, err
	}

	dir := cacheDir
	if dir == "" {
		var err error
		if dir, err = doclingclient.DefaultCacheDir(); err != nil {
			fmt.Fprintf(os.Stderr, "docli: cache disabled (no XDG dir): %v\n", err)
			resp, err := runConvert(ctx, c, input, opts)
			return resp, false, err
		}
	}

	version, versionKey, err := doclingclient.ServerVersion(ctx, c, dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "docli: cache disabled (no server version): %v\n", err)
		resp, err := runConvert(ctx, c, input, opts)
		return resp, false, err
	}

	fc, err := doclingclient.NewFileCache(dir, versionKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "docli: cache disabled: %v\n", err)
		resp, err := runConvert(ctx, c, input, opts)
		return resp, false, err
	}
	_ = fc.WriteVersionInfo(version)

	src, err := sourceForKey(input)
	if err != nil {
		return nil, false, err
	}
	key := doclingclient.CacheKey([]doclingclient.Source{src}, opts)

	if resp, ok, gerr := fc.Get(key); gerr == nil && ok {
		return resp, true, nil
	} else if gerr != nil {
		fmt.Fprintf(os.Stderr, "docli: cache read failed: %v\n", gerr)
	}

	resp, err := runConvert(ctx, c, input, opts)
	if err != nil {
		return nil, false, err
	}
	if perr := fc.Put(key, resp); perr != nil {
		fmt.Fprintf(os.Stderr, "docli: cache write failed: %v\n", perr)
	}
	return resp, false, nil
}

// sourceForKey returns a Source suitable for cache-key derivation only. For
// URLs it returns the HTTP source as-is; for local files it returns a
// SHA-256-fingerprinted synthetic Source (no file body inlined).
func sourceForKey(input string) (doclingclient.Source, error) {
	if isURL(input) {
		return doclingclient.NewHTTPSource(input), nil
	}
	return doclingclient.SourceForFile(input)
}

func writeContent(w io.Writer, doc doclingclient.Document, format string) error {
	switch format {
	case doclingclient.FormatJSON, doclingclient.FormatYAML:
		if len(doc.JSONContent) == 0 {
			return fmt.Errorf("server returned no %s content", format)
		}
		_, err := w.Write(doc.JSONContent)
		return err
	}
	var content string
	switch format {
	case doclingclient.FormatMD:
		content = doc.MDContent
	case doclingclient.FormatHTML:
		content = doc.HTMLContent
	case doclingclient.FormatText:
		content = doc.TextContent
	case doclingclient.FormatDoctags:
		content = doc.DoctagsContent
	default:
		return fmt.Errorf("unknown output format: %s", format)
	}
	if content == "" {
		return fmt.Errorf("server returned no %s content", format)
	}
	_, err := io.WriteString(w, content)
	return err
}

func isURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https")
}

func splitComma(s string) []string {
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parsePageRange(s string) ([]int, error) {
	if strings.Contains(s, "-") {
		var lo, hi int
		if _, err := fmt.Sscanf(s, "%d-%d", &lo, &hi); err != nil {
			return nil, fmt.Errorf("invalid page range %q: %w", s, err)
		}
		return []int{lo, hi}, nil
	}
	var p int
	if _, err := fmt.Sscanf(s, "%d", &p); err != nil {
		return nil, fmt.Errorf("invalid page %q: %w", s, err)
	}
	return []int{p, p}, nil
}

func validateOutputFormats(formats []string) error {
	for _, f := range formats {
		switch strings.TrimSpace(f) {
		case doclingclient.FormatMD,
			doclingclient.FormatJSON,
			doclingclient.FormatYAML,
			doclingclient.FormatHTML,
			doclingclient.FormatText,
			doclingclient.FormatDoctags:
		default:
			return fmt.Errorf("invalid --to format %q (want md, json, yaml, html, text, or doctags)", f)
		}
	}
	return nil
}

func validateStatusFormat(f string) error {
	switch f {
	case "text", "json":
		return nil
	}
	return fmt.Errorf("invalid --status-format %q (want text or json)", f)
}

// writeStatus emits one status line (or JSON object) per run to w. The "source"
// field is "fresh" or "cached" — whether the result came from the on-disk cache.
func writeStatus(w io.Writer, resp *doclingclient.ConvertResponse, cached bool, format string) error {
	origin := "fresh"
	if cached {
		origin = "cached"
	}
	if format == "json" {
		errs := resp.Errors
		if errs == nil {
			errs = []doclingclient.ErrorItem{}
		}
		payload := struct {
			Status         string                    `json:"status"`
			ProcessingTime float64                   `json:"processing_time"`
			Source         string                    `json:"source"`
			Filename       string                    `json:"filename,omitempty"`
			Errors         []doclingclient.ErrorItem `json:"errors"`
		}{
			Status:         resp.Status,
			ProcessingTime: resp.ProcessingTime,
			Source:         origin,
			Filename:       resp.Document.Filename,
			Errors:         errs,
		}
		enc := json.NewEncoder(w)
		return enc.Encode(payload)
	}
	if _, err := fmt.Fprintf(w, "status=%s processing_time=%.2fs source=%s\n", resp.Status, resp.ProcessingTime, origin); err != nil {
		return err
	}
	for _, e := range resp.Errors {
		if _, err := fmt.Fprintf(w, "error: [%s/%s] %s\n", e.ComponentType, e.ModuleName, e.ErrorMessage); err != nil {
			return err
		}
	}
	return nil
}

func validateImageExportMode(m string) error {
	switch m {
	case "",
		doclingclient.ImageExportPlaceholder,
		doclingclient.ImageExportEmbedded,
		doclingclient.ImageExportReferenced:
		return nil
	}
	return fmt.Errorf("invalid --image-export-mode %q (want placeholder, embedded, or referenced)", m)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
