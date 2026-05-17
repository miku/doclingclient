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
	"path/filepath"
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

	root.AddCommand(newConvertCmd(g), newChunkCmd(g), newHealthCmd(g, "health"), newHealthCmd(g, "ready"), newVersionCmd(g))
	return root, g
}

func newClient(g *globalOpts) *doclingclient.Client {
	var opts []doclingclient.Option
	if g.apiKey != "" {
		opts = append(opts, doclingclient.WithAPIKey(g.apiKey))
	}
	if g.tenantID != "" {
		opts = append(opts, doclingclient.WithTenantID(g.tenantID))
	}
	return doclingclient.New(g.server, opts...)
}

func newConvertCmd(g *globalOpts) *cobra.Command {
	var (
		toFormats    []string
		status       bool
		statusFormat string
		outputDir    string
		cacheDir     string
		noCache      bool
	)
	cmd := &cobra.Command{
		Use:   "convert <url-or-path>",
		Short: "Convert a document from a URL or local file",
		Long: `Convert a document via the docling-serve /v1/convert endpoints.

The argument is either an http(s) URL (sent as a remote source) or a local
path (streamed as multipart/form-data).

Output destinations:
  - Without --output: the first format listed in --to is written to stdout.
    Requesting multiple formats without --output prints a warning, since the
    extra formats have nowhere to go.
  - With --output <dir>: every requested format is written as
    <source-basename>.<ext> into <dir> and stdout stays silent.

Results are cached on disk by default, keyed by source content and every
option that affects output. Use --status to see whether a run was served
fresh or from cache, and --status-format json for ad-hoc post-processing.`,
		Args:    cobra.ExactArgs(1),
		Example: "  docli convert https://arxiv.org/pdf/2206.01062 > paper.md\n  docli convert --to json paper.pdf > paper.json\n  docli convert --to md,json,html --output ./out paper.pdf",
	}
	cmd.Flags().StringSliceVarP(&toFormats, "to", "t", []string{"md"}, "output formats: md, json, html, text, doctags")
	cmd.Flags().StringVarP(&outputDir, "output", "o", "", "directory to write all requested formats as <basename>.<ext>; stdout stays silent when set")
	convertFlags := addConvertOptionFlags(cmd)
	cmd.Flags().BoolVar(&status, "status", false, "print status and processing time to stderr")
	cmd.Flags().StringVar(&statusFormat, "status-format", "text", "format for --status output: text or json")
	cmd.Flags().StringVar(&cacheDir, "cache-dir", envOr("DOCLING_CACHE_DIR", ""), "cache directory (env DOCLING_CACHE_DIR, default XDG cache)")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "disable the on-disk result cache")

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if len(toFormats) == 0 {
			return fmt.Errorf("--to requires at least one format")
		}
		parsedTo, err := parseOutputFormats(toFormats)
		if err != nil {
			return err
		}
		if err := validateStatusFormat(statusFormat); err != nil {
			return err
		}
		primary := parsedTo[0]

		opts, err := convertFlags.build(cmd)
		if err != nil {
			return err
		}
		opts.ToFormats = parsedTo

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
		if err := resp.Err(false); err != nil {
			return err
		}
		if outputDir != "" {
			return writeOutputs(outputDir, resp.Document, parsedTo)
		}
		if len(parsedTo) > 1 {
			fmt.Fprintf(os.Stderr, "docli: only %q written to stdout (%d formats requested); pass --output <dir> to write all\n", primary, len(parsedTo))
		}
		return writeContent(cmd.OutOrStdout(), resp.Document, primary)
	}
	return cmd
}

func newChunkCmd(g *globalOpts) *cobra.Command {
	var (
		chunker        string
		maxTokens      int
		tokenizer      string
		mergePeers     bool
		mdTables       bool
		includeRawText bool
		pretty         bool
	)
	cmd := &cobra.Command{
		Use:   "chunk <url-or-path>",
		Short: "Chunk a document for embedding or RAG pipelines",
		Long: `Convert a document and split it into chunks via the docling-serve /v1/chunk endpoints.

Two chunker strategies are available:

- hybrid (default): tokenization-aware chunks on top of hierarchical
  splitting. Each chunk fits a token budget derived from --tokenizer or
  capped by --max-tokens. This is the chunker most RAG pipelines want.
- hierarchical: one chunk per detected document element (heading, paragraph,
  list item, table). No tokenizer involved; chunk sizes vary with document
  structure.

All conversion flags (--ocr, --pages, --pdf-backend, --pipeline, etc.) apply
the same way as for ` + "`docli convert`" + ` and tune the underlying conversion
before chunking.

Output is JSONL on stdout, one chunk per line. Use --pretty for indented JSON of the full response instead.`,
		Args:    cobra.ExactArgs(1),
		Example: "  docli chunk https://arxiv.org/pdf/2206.01062 > paper.jsonl\n  docli chunk --chunker hierarchical paper.pdf > paper.jsonl\n  docli chunk --max-tokens 512 --tokenizer Qwen/Qwen3-Embedding-0.6B paper.pdf",
	}
	convertFlags := addConvertOptionFlags(cmd)
	cmd.Flags().StringVar(&chunker, "chunker", "hybrid", "chunker: hybrid or hierarchical")
	cmd.Flags().IntVar(&maxTokens, "max-tokens", 0, "hybrid: max tokens per chunk (server default if unset)")
	cmd.Flags().StringVar(&tokenizer, "tokenizer", "", "hybrid: HuggingFace tokenizer model (server default if empty)")
	cmd.Flags().BoolVar(&mergePeers, "merge-peers", true, "hybrid: merge undersized successive chunks with same headings")
	cmd.Flags().BoolVar(&mdTables, "markdown-tables", false, "serialize tables as Markdown instead of triplets")
	cmd.Flags().BoolVar(&includeRawText, "include-raw-text", false, "populate raw_text alongside contextualized text")
	cmd.Flags().BoolVar(&pretty, "pretty", false, "emit the full response as indented JSON instead of JSONL of chunks")

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		input := args[0]
		convertOpts, err := convertFlags.build(cmd)
		if err != nil {
			return err
		}
		client := newClient(g)
		ctx := cmd.Context()

		var resp *doclingclient.ChunkResponse
		switch doclingclient.Chunker(chunker) {
		case doclingclient.ChunkerHybrid:
			opts := &doclingclient.HybridChunkerOptions{}
			if cmd.Flags().Changed("max-tokens") {
				opts.MaxTokens = doclingclient.Ptr(maxTokens)
			}
			if tokenizer != "" {
				opts.Tokenizer = tokenizer
			}
			if cmd.Flags().Changed("merge-peers") {
				opts.MergePeers = doclingclient.Ptr(mergePeers)
			}
			if cmd.Flags().Changed("markdown-tables") {
				opts.UseMarkdownTables = doclingclient.Ptr(mdTables)
			}
			if cmd.Flags().Changed("include-raw-text") {
				opts.IncludeRawText = doclingclient.Ptr(includeRawText)
			}
			if isURL(input) {
				resp, err = client.ChunkHybrid(ctx, []doclingclient.Source{doclingclient.NewHTTPSource(input)}, convertOpts, opts)
			} else {
				resp, err = client.ChunkHybridPath(ctx, input, convertOpts, opts)
			}
		case doclingclient.ChunkerHierarchical:
			opts := &doclingclient.HierarchicalChunkerOptions{}
			if cmd.Flags().Changed("markdown-tables") {
				opts.UseMarkdownTables = doclingclient.Ptr(mdTables)
			}
			if cmd.Flags().Changed("include-raw-text") {
				opts.IncludeRawText = doclingclient.Ptr(includeRawText)
			}
			if isURL(input) {
				resp, err = client.ChunkHierarchical(ctx, []doclingclient.Source{doclingclient.NewHTTPSource(input)}, convertOpts, opts)
			} else {
				resp, err = client.ChunkHierarchicalPath(ctx, input, convertOpts, opts)
			}
		default:
			return fmt.Errorf("invalid --chunker %q (want hybrid or hierarchical)", chunker)
		}
		if err != nil {
			return err
		}

		enc := json.NewEncoder(cmd.OutOrStdout())
		if pretty {
			enc.SetIndent("", "  ")
			return enc.Encode(resp)
		}
		for _, c := range resp.Chunks {
			if err := enc.Encode(c); err != nil {
				return err
			}
		}
		return nil
	}
	return cmd
}

func newHealthCmd(g *globalOpts, name string) *cobra.Command {
	long := `Call GET /` + name + ` on docling-serve and print the reported status.

/health is a liveness probe (is the process up?); /ready is a readiness probe
(can the process actually serve requests, e.g. are models loaded?). Both
return a tiny {"status": "..."} JSON; the exit status reflects success or a
transport/HTTP error.`
	return &cobra.Command{
		Use:   name,
		Short: "Check the docling-serve /" + name + " endpoint",
		Long:  long,
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
		Long: `Fetch and print the docling-serve /version response as indented JSON.

This reports the server build (docling-serve, docling-core, model versions,
etc.) and is independent from the CLI version. For the docli build, use the
top-level --version flag instead.`,
		Args: cobra.NoArgs,
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

// formatExtension returns the file extension docli uses when writing a given
// format to disk. Returns "" for unknown formats; ParseOutputFormat should
// have rejected those before we get here.
func formatExtension(f doclingclient.OutputFormat) string {
	switch f {
	case doclingclient.FormatMD:
		return ".md"
	case doclingclient.FormatJSON:
		return ".json"
	case doclingclient.FormatHTML:
		return ".html"
	case doclingclient.FormatText:
		return ".txt"
	case doclingclient.FormatDoctags:
		return ".doctags"
	}
	return ""
}

// writeOutputs writes one file per requested format into dir, named after the
// server-reported source filename (extension stripped). Errors out before
// writing anything if any format's content is empty.
func writeOutputs(dir string, doc doclingclient.Document, formats []doclingclient.OutputFormat) error {
	base := strings.TrimSuffix(doc.Filename, filepath.Ext(doc.Filename))
	if base == "" {
		base = "output"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, f := range formats {
		var content []byte
		switch f {
		case doclingclient.FormatJSON:
			content = doc.JSONContent
		case doclingclient.FormatMD:
			content = []byte(doc.MDContent)
		case doclingclient.FormatHTML:
			content = []byte(doc.HTMLContent)
		case doclingclient.FormatText:
			content = []byte(doc.TextContent)
		case doclingclient.FormatDoctags:
			content = []byte(doc.DoctagsContent)
		default:
			return fmt.Errorf("unknown output format: %s", f)
		}
		if len(content) == 0 {
			return fmt.Errorf("server returned no %s content", f)
		}
		path := filepath.Join(dir, base+formatExtension(f))
		if err := os.WriteFile(path, content, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func writeContent(w io.Writer, doc doclingclient.Document, format doclingclient.OutputFormat) error {
	if format == doclingclient.FormatJSON {
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

// parseOutputFormats validates and types every --to value, preserving order.
func parseOutputFormats(formats []string) ([]doclingclient.OutputFormat, error) {
	out := make([]doclingclient.OutputFormat, len(formats))
	for i, s := range formats {
		f, err := doclingclient.ParseOutputFormat(s)
		if err != nil {
			return nil, fmt.Errorf("--to: %w", err)
		}
		out[i] = f
	}
	return out, nil
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
			Status:         string(resp.Status),
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

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// convertOptFlags holds the conversion-tuning flag values shared by both
// `docli convert` and `docli chunk`. Register them with addConvertOptionFlags
// and build the corresponding *Options with build.
type convertOptFlags struct {
	fromFormats     []string
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
	pdfBackend      string
	pipeline        string
}

// addConvertOptionFlags registers the flag block on cmd and returns the
// backing storage. Defaults match the existing convert command; the build()
// pass uses cmd.Flags().Changed() to keep server defaults intact for bool
// and numeric flags the user didn't touch.
func addConvertOptionFlags(cmd *cobra.Command) *convertOptFlags {
	f := &convertOptFlags{}
	cmd.Flags().StringSliceVar(&f.fromFormats, "from", nil, "input formats (e.g. pdf,docx); server autodetects if empty")
	cmd.Flags().BoolVar(&f.ocr, "ocr", true, "enable OCR")
	cmd.Flags().BoolVar(&f.forceOCR, "force-ocr", false, "force OCR over existing text")
	cmd.Flags().StringVar(&f.ocrLang, "ocr-lang", "", "comma-separated OCR languages, e.g. 'en,de'")
	cmd.Flags().StringVar(&f.tableMode, "table-mode", "", "table mode: fast or accurate (server default if empty)")
	cmd.Flags().StringVar(&f.pages, "pages", "", "page range, e.g. '1-10' or '3'")
	cmd.Flags().StringVar(&f.imageExportMode, "image-export-mode", "", "image export mode for image-capable outputs: placeholder, embedded, referenced (server default if empty)")
	cmd.Flags().BoolVar(&f.abortOnError, "abort-on-error", false, "abort the conversion on the first error")
	cmd.Flags().Float64Var(&f.docTimeout, "document-timeout", 0, "per-document timeout in seconds (server default if unset)")
	cmd.Flags().BoolVar(&f.doTables, "tables", true, "extract table structure")
	cmd.Flags().Float64Var(&f.imagesScale, "images-scale", 0, "scale factor for extracted images (server default 2.0 if unset)")
	cmd.Flags().BoolVar(&f.includeImages, "include-images", true, "include images extracted from the document")
	cmd.Flags().StringVar(&f.pdfBackend, "pdf-backend", "", "pdf backend: pypdfium2, docling_parse, dlparse_v1, dlparse_v2, dlparse_v4 (server default if empty)")
	cmd.Flags().StringVar(&f.pipeline, "pipeline", "", "processing pipeline: legacy, standard, vlm, asr (server default if empty)")
	return f
}

// build constructs an *Options from the parsed flags. Numeric/bool flags with
// non-zero CLI defaults are sent only when the user explicitly set them, so
// the server's own defaults stay authoritative on bare invocations.
func (f *convertOptFlags) build(cmd *cobra.Command) (*doclingclient.Options, error) {
	imageMode, err := doclingclient.ParseImageExportMode(f.imageExportMode)
	if err != nil {
		return nil, fmt.Errorf("--image-export-mode: %w", err)
	}
	tableMode, err := doclingclient.ParseTableMode(f.tableMode)
	if err != nil {
		return nil, fmt.Errorf("--table-mode: %w", err)
	}
	pdfBackend, err := doclingclient.ParsePDFBackend(f.pdfBackend)
	if err != nil {
		return nil, fmt.Errorf("--pdf-backend: %w", err)
	}
	pipeline, err := doclingclient.ParsePipeline(f.pipeline)
	if err != nil {
		return nil, fmt.Errorf("--pipeline: %w", err)
	}

	opts := &doclingclient.Options{
		FromFormats:     f.fromFormats,
		DoOCR:           doclingclient.Ptr(f.ocr),
		ForceOCR:        doclingclient.Ptr(f.forceOCR),
		TableMode:       tableMode,
		ImageExportMode: imageMode,
		PDFBackend:      pdfBackend,
		Pipeline:        pipeline,
	}
	if f.ocrLang != "" {
		opts.OCRLang = splitComma(f.ocrLang)
	}
	if f.pages != "" {
		r, err := parsePageRange(f.pages)
		if err != nil {
			return nil, err
		}
		opts.PageRange = r
	}
	if cmd.Flags().Changed("abort-on-error") {
		opts.AbortOnError = doclingclient.Ptr(f.abortOnError)
	}
	if cmd.Flags().Changed("tables") {
		opts.DoTableStructure = doclingclient.Ptr(f.doTables)
	}
	if cmd.Flags().Changed("include-images") {
		opts.IncludeImages = doclingclient.Ptr(f.includeImages)
	}
	if cmd.Flags().Changed("document-timeout") {
		opts.DocumentTimeout = doclingclient.Ptr(f.docTimeout)
	}
	if cmd.Flags().Changed("images-scale") {
		opts.ImagesScale = doclingclient.Ptr(f.imagesScale)
	}
	return opts, nil
}
