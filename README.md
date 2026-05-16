# doclingclient

A Go [docling](https://www.docling.ai/) client library and CLI.
[Docling](https://www.docling.ai/) is a document conversion project, which can
also be run [as service](https://github.com/docling-project/docling-serve).
This helps to decouple the document processing, which may benefit from a GPU,
from the client, which may be a lower spec machine.

Docling serve supplies an openapi spec, currently using version 3.1.0.

```
$ jq -rc '.paths | keys[]' openapi.json
/health
/openapi-3.0.json
/ready
/v1/chunk/hierarchical/file
/v1/chunk/hierarchical/file/async
/v1/chunk/hierarchical/source
/v1/chunk/hierarchical/source/async
/v1/chunk/hybrid/file
/v1/chunk/hybrid/file/async
/v1/chunk/hybrid/source
/v1/chunk/hybrid/source/async
/v1/clear/converters
/v1/clear/results
/v1/convert/file
/v1/convert/file/async
/v1/convert/source
/v1/convert/source/async
/v1/memory/counts
/v1/memory/stats
/v1/result/{task_id}
/v1/status/poll/{task_id}
/version
```

Unfortunately, an SDK from a spec can be quite large and may have downsides;
for a comparison, see [this
comparison](https://www.speakeasy.com/docs/sdks/languages/golang/oss-comparison-go#go-sdk-generator-options).

Hence, we fall back to a more manual approach. Use an LLM to build a simple,
idiomatic client for the core functionality first. For docling this may be just
"/v1/convert/file" and "/v1/convert/source" - this would already serve most use
cases.

Create a minimal Go library first, then wrap a nice CLI around the library, so
interacting with the docling service becomes easy to integrate into shell
scripts or ad-hoc human (and maybe agentic) terminal use.

**Status**: Library and CLI cover synchronous conversion (`/v1/convert/source`
and `/v1/convert/file`), the `/health`, `/ready`, and `/version` routes. Async
conversion and the chunking endpoints are not yet wrapped.

**Requirements**: Go 1.22+. A running `docling-serve` instance (defaults to
`http://localhost:5001`).


## Library

```go
import "github.com/miku/doclingclient"

c := doclingclient.New("http://localhost:5001",
    doclingclient.WithAPIKey("sk-..."),
    doclingclient.WithTimeout(10*time.Minute),
)

// Convert a URL.
resp, err := c.ConvertURL(ctx, "https://arxiv.org/pdf/2206.01062", nil)

// Convert a local file (streamed multipart upload).
resp, err := c.ConvertPath(ctx, "paper.pdf", &doclingclient.Options{
    ToFormats: []doclingclient.OutputFormat{doclingclient.FormatMD, doclingclient.FormatJSON},
    DoOCR:     doclingclient.Ptr(true),
    Pipeline:  doclingclient.PipelineStandard,
})

// A 200 response can still describe a conversion failure — check it.
if err := resp.Err(false); err != nil {
    log.Fatal(err)
}
fmt.Println(resp.Document.MDContent)
```

The library covers `/v1/convert/source` (URL or base64 in-body), `/v1/convert/file`
(streamed multipart upload), and the `/health`, `/ready`, `/version` routes.
For full coverage of `ConvertDocumentsOptions`, the struct in `types.go` is a
deliberate subset — extend it as needed.

Note on output formats: the docling-serve `OutputFormat` enum also defines
`yaml`, `html_split_page`, and `vtt`, but the `ExportDocumentResponse` object
does not carry corresponding content fields, so this library and CLI do not
surface them. The five exposed formats (`md`, `json`, `html`, `text`,
`doctags`) match what the server actually returns.

## CLI

A minimal command, `docli`, wraps the library. It is named to avoid collision
with the upstream `docling` CLI.

```sh
go install github.com/miku/doclingclient/cmd/docli@latest

# Convert a URL (default output: markdown to stdout).
docli convert https://arxiv.org/pdf/2206.01062 > paper.md

# Convert a local file as JSON.
docli convert --to json paper.pdf > paper.json

# Produce several formats at once and write them to a directory.
docli convert --to md,json,html --output ./out paper.pdf
# => ./out/paper.md, ./out/paper.json, ./out/paper.html

# Talk to a remote docling-serve, with auth.
DOCLING_SERVER=https://docling.example.org \
DOCLING_API_KEY=sk-... \
    docli convert paper.pdf

# Server checks.
docli health
docli ready
docli version
```

### `docli convert` flags

| Flag                  | Default | Description                                                                       |
|-----------------------|---------|-----------------------------------------------------------------------------------|
| `--from`              | (auto)  | Input formats, e.g. `pdf,docx`; server autodetects if empty.                      |
| `--to`, `-t`          | `md`    | Output formats: `md`, `json`, `html`, `text`, `doctags`.                          |
| `--output`, `-o`      | (none)  | Directory to write all requested formats as `<basename>.<ext>`; stdout is silent. |
| `--ocr`               | `true`  | Enable OCR.                                                                       |
| `--force-ocr`         | `false` | Force OCR over existing text.                                                     |
| `--ocr-lang`          | (auto)  | Comma-separated OCR languages, e.g. `en,de`.                                      |
| `--table-mode`        | (auto)  | `fast` or `accurate`; server default if empty.                                    |
| `--tables`            | (auto)  | Extract table structure. Sent only when explicitly set.                           |
| `--pages`             | (all)   | Page range, e.g. `1-10` or `3`.                                                   |
| `--image-export-mode` | (auto)  | `placeholder`, `embedded`, or `referenced`. Server default if empty.              |
| `--include-images`    | (auto)  | Include extracted images. Sent only when explicitly set.                          |
| `--images-scale`      | (auto)  | Scale factor for extracted images (server default ~2.0).                          |
| `--abort-on-error`    | `false` | Abort on first error. Sent only when explicitly set.                              |
| `--document-timeout`  | (none)  | Per-document timeout in seconds.                                                  |
| `--status`            | `false` | Emit one status line/object to stderr after the conversion.                       |
| `--status-format`     | `text`  | `text` or `json` (see Caching below).                                             |
| `--cache-dir`         | (XDG)   | Override the on-disk cache directory. Env: `DOCLING_CACHE_DIR`.                   |
| `--no-cache`          | `false` | Disable the on-disk result cache.                                                 |

Global flags (any subcommand): `--server`/`-s` (env `DOCLING_SERVER`),
`--api-key`/`-K` (env `DOCLING_API_KEY`), `--tenant`/`-T` (env
`DOCLING_TENANT_ID`).

## Caching

`docli convert` caches results on disk by default, so repeating a request is
near-instant. The cache uses the XDG spec, typically
`~/.cache/doclingclient/`, overridable with `--cache-dir` or
`DOCLING_CACHE_DIR`. Disable with `--no-cache`.

Layout:

```
~/.cache/doclingclient/
├── _server_version.json          # /version response, refreshed every 24 h
└── <12-char-server-hash>/
    ├── _info.json                # full server version map for this namespace
    └── <input-hash>.json.zst     # zstd-compressed ConvertResponse JSON
```

Cache key fingerprints everything that affects output: source URL or local
file content (SHA-256), `to_formats`, OCR settings, table mode, page range,
etc. The server-version directory namespaces cached results, so an upstream
docling-serve upgrade naturally falls into a fresh namespace — old results
stay around for diffing or can be pruned with `rm -rf
~/.cache/doclingclient/<hash>/`.

Use `--status` to see whether a request was served fresh or from cache:

```sh
$ docli convert --status paper.pdf > /dev/null
status=success processing_time=12.43s source=fresh
$ docli convert --status paper.pdf > /dev/null
status=success processing_time=12.43s source=cached
```

For ad-hoc post-processing, add `--status-format json` to emit a single JSON
object per run to stderr (one line, suitable for `jq` or appending to a log):

```sh
$ docli convert --status --status-format json paper.pdf > paper.md
{"status":"success","processing_time":12.43,"source":"fresh","filename":"paper.pdf","errors":[]}

$ docli convert --status --status-format json paper.pdf 2> status.jsonl > paper.md
$ jq -r '.processing_time' < status.jsonl
12.43
```

## Testing

```sh
go test ./...
go test -cover ./...
```

The library exercises its HTTP client against `httptest.Server`; no live
docling-serve instance is required.

## A random thought on openapi

[OpenAPI](https://en.wikipedia.org/wiki/OpenAPI_Specification) was very helpful
to get this client started, in that the LLM could inquire the
[openapi.json](openapi.json) file for the spec. However, we did not need to use
any of the openapi generators, of which there are [quite a
few](https://www.speakeasy.com/docs/sdks/languages/golang/oss-comparison-go). A
more systematic comparison of features of various libraries is still
outstanding, but you could see an LLM + Prompt + openapi.json based client SDK
generator.

