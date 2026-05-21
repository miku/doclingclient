# doclingclient

A Go [docling](https://www.docling.ai/) client library and CLI.
[Docling](https://www.docling.ai/) is a deep learning document analysis and
conversion project, which can also be run [as
service](https://github.com/docling-project/docling-serve). This project helps
to decouple the document processing, which may benefit from a GPU, from the
client, which may be a lower spec machine.

[![](static/Europeana.eu-90402-RP_T_2013_34_6_R_-98a3ebb632a8943697d57d5193f12634-s.jpeg)](https://www.europeana.eu/en/item/90402/RP_T_2013_34_6_R_)

## Installation

```shell
$ go install github.com/miku/doclingclient/cmd/docli@latest
```

Packages (deb, rpm), cf.
[releases](https://github.com/miku/doclingclient/releases). Quick start:

```shell
$ docli --server http://docling.city:5001 convert https://arxiv.org/pdf/2110.06595
```


## Background, Prompt

[Docling serve](https://github.com/docling-project/docling-serve) supplies an [openapi](openapi.json) spec, currently using version
3.1.0 of the standard.

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

Unfortunately, an SDK generated from a spec can be quite large and may have
downsides; cf. also [this
comparison](https://www.speakeasy.com/docs/sdks/languages/golang/oss-comparison-go#go-sdk-generator-options).

Hence, we decided to use a more manual approach. We use an LLM to build a
simple, mostly idiomatic client for the core functionality first. For docling this may
be just "/v1/convert/file" and "/v1/convert/source" - this would already serve
most use cases.

Create a minimal Go library, then wrap a nice CLI around the library, so
interacting with the docling service becomes easy to integrate into shell
scripts or ad-hoc human (and maybe agentic) terminal use.

**Status**: Library and CLI cover synchronous conversion
`/v1/convert/{source,file}`, synchronous chunking
`/v1/chunk/{hybrid,hierarchical}/{source,file}`, and the `/health`, `/ready`,
and `/version` routes. Async conversion and async chunking are not yet wrapped.

**Breaking changes in v0.2**: the request shape was reorganised to match the
[iguanesolutions/go-docling](https://github.com/iguanesolutions/go-docling)
style. `Convert` / `ConvertFile` / `ConvertWithTarget` / `ConvertFileWithTarget`
were replaced by `ProcessURL(ctx, ProcessURLRequest)` and `ProcessFile(ctx,
ProcessFileRequest)`; `Options` was renamed to `ConvertOptions` and is now
passed by value (zero = all server defaults); `FileUpload` was replaced by the
`File` interface (`Name() string + io.Reader`) and `FileReader` helper.
`Document` exposes `MarkdownContent()` / `JSONContent()` / `HTMLContent()` /
`TextContent()` / `DoctagsContent()` accessors instead of flat `MDContent`
fields. Server-default-false bools (`ForceOCR`, `AbortOnError`) are now plain
`bool`; only fields whose server default is `true` keep `*bool` for explicit
override. The `ConvertURL` / `ConvertPath` / `ConvertReader` helpers are
unchanged in spirit but take `ConvertOptions` by value. The on-disk cache
namespace stays version-keyed, so an upstream `docling-serve` upgrade rolls
into a fresh dir; existing v0.1 entries are not read by v0.2.

**Requirements**: Go 1.24+. A running `docling-serve` instance (defaults to `http://localhost:5001`).

## Library

```go
import "github.com/miku/doclingclient"

c := doclingclient.New("http://localhost:5001",
    doclingclient.WithAPIKey("sk-..."),
    doclingclient.WithTimeout(10*time.Minute),
)

// Convert a URL.
resp, err := c.ConvertURL(ctx, "https://arxiv.org/pdf/2206.01062", doclingclient.ConvertOptions{})

// Convert a local file (streamed multipart upload).
resp, err := c.ConvertPath(ctx, "paper.pdf", doclingclient.ConvertOptions{
    ToFormats: []doclingclient.OutputFormat{
                    doclingclient.FormatMD,
                    doclingclient.FormatJSON},
    DoOCR:     doclingclient.Ptr(true),
    Pipeline:  doclingclient.PipelineStandard,
})

// A 200 response can still describe a conversion failure — check it.
if err := resp.Err(false); err != nil {
    log.Fatal(err)
}
fmt.Println(resp.Document.MarkdownContent())

// Single-struct request: redirect the result with an explicit delivery
// target. The server defaults to inbody; use PutTarget / S3Target / ZipTarget
// on /v1/convert/source. The multipart /v1/convert/file endpoint only
// supports inbody and zip, expressed as TargetTypeInBody / TargetTypeZip via
// ProcessFileRequest.TargetType.
resp, err = c.ProcessURL(ctx, doclingclient.ProcessURLRequest{
    Sources: []doclingclient.Source{
        doclingclient.NewHTTPSource("https://arxiv.org/pdf/2206.01062"),
    },
    Target: doclingclient.PutTarget{URL: "https://sink.example/result"},
})
```

The library covers `/v1/convert/source` (URL or base64 in-body), `/v1/convert/file`
(streamed multipart upload), and the `/health`, `/ready`, `/version` routes.
For full coverage of `ConvertDocumentsOptions`, the struct in `types.go` is a
deliberate subset — extend it as needed.

Note on output formats: the docling-serve `OutputFormat` enum also defines
`yaml`, `html_split_page`, and `vtt`, but the `ExportDocumentResponse` object
does not carry corresponding content fields, so this library and CLI do not
surface them. The five exposed formats: `md`, `json`, `html`, `text`,
`doctags` match what the server actually returns.

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

### Chunking for RAG / embeddings

`docli chunk` converts a document and splits it into chunks suitable for
feeding into an embedding model. Output is JSONL on stdout — one chunk per
line — which composes naturally with `jq`.

```sh
# Default hybrid chunker (tokenization-aware).
docli chunk paper.pdf > chunks.jsonl

# Pick a tokenizer and cap chunks to 512 tokens.
docli chunk --max-tokens 512 \
    --tokenizer Qwen/Qwen3-Embedding-0.6B \
    paper.pdf > chunks.jsonl

# Structural chunks (one per document element, no tokenizer).
docli chunk --chunker hierarchical paper.pdf > chunks.jsonl

# Inspect chunk lengths.
jq -r '.num_tokens // (.text | length)' < chunks.jsonl | sort -n | uniq -c
```

Each chunk carries `text` (with headings/captions inlined for context),
optional `raw_text` (with `--include-raw-text`), `num_tokens`, `headings`,
`captions`, `page_numbers`, and `doc_items` references into the source
document.

#### Tokenizer choice

The hybrid chunker counts tokens to keep each chunk within a budget. That
budget is meaningful only relative to a specific tokenizer — and you almost
always want the tokenizer to match the embedding model you'll feed the chunks
into downstream, so chunk sizes line up with the embedder's context window.

docling-serve accepts any HuggingFace tokenizer identifier as `--tokenizer`
(OpenAI/tiktoken tokenizers are not reachable through the server). The default
is `sentence-transformers/all-MiniLM-L6-v2`. If you don't pass `--max-tokens`,
the cap is derived from the tokenizer's `model_max_length`.

A few common picks, biased toward what shows up in docling's own examples and
typical RAG stacks:

| Tokenizer (HuggingFace ID)                  | Max tokens | Notes                                                    |
|---------------------------------------------|------------|----------------------------------------------------------|
| `sentence-transformers/all-MiniLM-L6-v2`    | 256        | Default. Tiny, fast, English-only. Good baseline.        |
| `sentence-transformers/all-mpnet-base-v2`   | 384        | Higher-quality English embeddings, still small.          |
| `BAAI/bge-small-en-v1.5`                    | 512        | Strong small English model, widely used in RAG.          |
| `BAAI/bge-m3`                               | 8192       | Multilingual, long-context. Good general-purpose pick.   |
| `intfloat/multilingual-e5-large`            | 512        | Multilingual, balanced quality/size.                     |
| `nomic-ai/nomic-embed-text-v1.5`            | 8192       | Long-context English.                                    |
| `Qwen/Qwen3-Embedding-0.6B`                 | 32768      | Long-context, multilingual, newer.                       |

Rule of thumb: pick the tokenizer that ships with the embedding model you
plan to call after `docli chunk`. Mixing them silently misaligns the token
count and leads to chunks that overflow (or underfill) the real embedder.

The server needs to fetch the tokenizer the first time it sees it. In
air-gapped deployments only models already cached on the server will work.

### Conversion flags (shared by `convert` and `chunk`)

These flags tune the underlying document conversion. They apply identically
to `docli convert` and `docli chunk`. Numeric and boolean defaults marked
`(auto)` are sent only when you set them explicitly, so docling-serve's own
defaults stay authoritative on bare invocations.

| Flag                  | Default | Description                                                            |
|-----------------------|---------|------------------------------------------------------------------------|
| `--from`              | (auto)  | Input formats, e.g. `pdf,docx`; server autodetects if empty.           |
| `--ocr`               | `true`  | Enable OCR.                                                            |
| `--force-ocr`         | `false` | Force OCR over existing text.                                          |
| `--ocr-lang`          | (auto)  | Comma-separated OCR languages, e.g. `en,de`.                           |
| `--table-mode`        | (auto)  | `fast` or `accurate`; server default if empty.                         |
| `--tables`            | (auto)  | Extract table structure. Sent only when explicitly set.                |
| `--pages`             | (all)   | Page range, e.g. `1-10` or `3`.                                        |
| `--image-export-mode` | (auto)  | `placeholder`, `embedded`, or `referenced`. Server default if empty.   |
| `--include-images`    | (auto)  | Include extracted images. Sent only when explicitly set.               |
| `--images-scale`      | (auto)  | Scale factor for extracted images (server default ~2.0).               |
| `--abort-on-error`    | `false` | Abort on first error. Sent only when explicitly set.                   |
| `--document-timeout`  | (none)  | Per-document timeout in seconds.                                       |
| `--pdf-backend`       | (auto)  | `pypdfium2`, `docling_parse`, `dlparse_v1`, `dlparse_v2`, `dlparse_v4`.|
| `--pipeline`          | (auto)  | `legacy`, `standard`, `vlm`, or `asr`. Server default if empty.        |

### `docli convert` extras

| Flag              | Default | Description                                                                       |
|-------------------|---------|-----------------------------------------------------------------------------------|
| `--to`, `-t`      | `md`    | Output formats: `md`, `json`, `html`, `text`, `doctags`.                          |
| `--output`, `-o`  | (none)  | Directory to write all requested formats as `<basename>.<ext>`; stdout is silent. |
| `--status`        | `false` | Emit one status line/object to stderr after the conversion.                       |
| `--status-format` | `text`  | `text` or `json` (see Caching below).                                             |
| `--cache-dir`     | (XDG)   | Override the on-disk cache directory. Env: `DOCLING_CACHE_DIR`.                   |
| `--no-cache`      | `false` | Disable the on-disk result cache.                                                 |

### `docli chunk` extras

| Flag                 | Default                                  | Description                                                              |
|----------------------|------------------------------------------|--------------------------------------------------------------------------|
| `--chunker`          | `hybrid`                                 | Chunker strategy: `hybrid` or `hierarchical`.                            |
| `--max-tokens`       | (auto)                                   | Hybrid only. Max tokens per chunk; derived from the tokenizer if unset.  |
| `--tokenizer`        | `sentence-transformers/all-MiniLM-L6-v2` | Hybrid only. HuggingFace tokenizer ID. See "Tokenizer choice" above.     |
| `--merge-peers`      | `true`                                   | Hybrid only. Merge undersized successive chunks with the same headings.  |
| `--markdown-tables`  | `false`                                  | Serialize tables as Markdown instead of triplets.                        |
| `--include-raw-text` | `false`                                  | Populate `raw_text` on each chunk alongside the contextualized `text`.   |
| `--pretty`           | `false`                                  | Emit the full response as indented JSON instead of one chunk per line.   |

Note: `docli chunk` does not cache results; each invocation re-runs the
conversion server-side. Only `docli convert` uses the on-disk cache.

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
├── server_version.json           # /version response, refreshed every 24 h
└── <12-char-server-hash>/
    ├── server_info.json           # full server version map for this namespace
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

## Other projects

* [https://github.com/iguanesolutions/go-docling](https://github.com/iguanesolutions/go-docling)

## A random thought on openapi

[OpenAPI](https://en.wikipedia.org/wiki/OpenAPI_Specification) was very helpful
to get this client started, in that the LLM could inquire the
[openapi.json](openapi.json) file for the spec. However, we did not need to use
any of the openapi generators, of which there are [quite a
few](https://www.speakeasy.com/docs/sdks/languages/golang/oss-comparison-go). A
more systematic comparison of features of various libraries is still
outstanding, but you could see an LLM + Prompt + openapi.json based client SDK
generator.


