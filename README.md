# doclingclient

A Go docling client library and CLI. [Docling](https://www.docling.ai/) is a
document conversion project, which can also be run [as
service](https://github.com/docling-project/docling-serve). That decouples the
processing, which may benefit from a GPU, from the client, which may be a lower
spec machine.

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


## Library

```go
import "github.com/miku/doclingclient"

c := doclingclient.New("http://localhost:5001")

// Convert a URL.
resp, err := c.ConvertURL(ctx, "https://arxiv.org/pdf/2206.01062", nil)

// Convert a local file (streamed multipart upload).
resp, err := c.ConvertPath(ctx, "paper.pdf", &doclingclient.Options{
    ToFormats: []string{doclingclient.FormatMD, doclingclient.FormatJSON},
    DoOCR:     doclingclient.Ptr(true),
})

fmt.Println(resp.Document.MDContent)
```

The library covers `/v1/convert/source` (URL or base64 in-body), `/v1/convert/file`
(streamed multipart upload), and the `/health`, `/ready`, `/version` routes.
For full coverage of `ConvertDocumentsOptions`, the struct in `types.go` is a
deliberate subset — extend it as needed.

## CLI

A minimal command, `docli`, wraps the library. It is named to avoid collision
with the upstream `docling` CLI.

```sh
go install github.com/miku/doclingclient/cmd/docli@latest

# Convert a URL (default output: markdown to stdout).
docli convert https://arxiv.org/pdf/2206.01062 > paper.md

# Convert a local file as JSON.
docli convert --to json paper.pdf > paper.json

# Talk to a remote docling-serve, with auth.
DOCLING_SERVER=https://docling.example.org \
DOCLING_API_KEY=sk-... \
    docli convert paper.pdf

# Server checks.
docli health
docli ready
docli version
```

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

