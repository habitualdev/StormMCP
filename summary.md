# storm-mcp: Summary

storm-mcp is a Model Context Protocol server for writing, validating, and understanding **Storm** — the query language for Vertex Synapse. It exposes eight tools to Claude backed by three ONNX models, a pre-computed vector store, a clean code corpus, a tagged query library, and the full Synapse data model.

---

## Models

### all-MiniLM-L6-v2 (`model/all-MiniLM-L6-v2.onnx`)
Sentence embedding model (384-dim). Used for two purposes:
- **Doc search**: queries are embedded at runtime and compared by cosine similarity against `synapse_db/vectors.bin`, which holds pre-computed embeddings for every documentation chunk.
- **Code example search**: the same embedder ranks chunks in the clean Storm corpus (`corpus/clean_chunks.json`).

### Storm lint classifier (`model/binary_nozip.onnx`)
A LightGBM binary classifier (serialised as ONNX) trained on labelled Storm code. Ingests a fixed feature vector extracted from the lexed token stream and outputs a `clean` / `violation` label, a confidence score, and the predicted rule code (e.g. `W002`, `I007`).

### CodeT5-base query generator (`model/encoder.onnx.gz` + `model/decoder.onnx.gz`)
INT8-quantised, gzip-compressed seq2seq model fine-tuned for Storm query synthesis. Task prefix: `generate storm: `. Uses RoBERTa byte-level BPE tokenisation with Storm-specific added tokens. Greedy decoding up to 256 tokens. Only available in the `storm-mcp-full` binary (built with `-tags embedquery`); the base binary reports the tool as unavailable.

---

## Data Assets

| Path | Contents |
|---|---|
| `synapse_db/vectors.bin` | Pre-computed MiniLM embeddings for every Synapse doc chunk |
| `synapse_db/metadata.json` | Chunk metadata (source, heading, chunk type) |
| `corpus/clean_chunks.json` | Labelled clean Storm code examples |
| `corpus/tagged_queries.json` | Curated Storm queries tagged by intent and technique |
| `forms.json` | 592 Synapse data model form definitions (properties, types, edges) |

All assets are embedded into the binary at compile time via `//go:embed`. Model and data files are stored in Git LFS.

---

## Tools

| Tool | When to use |
|---|---|
| `get_form_info` | Look up a Synapse form — properties, secondary properties, edges. Start here before writing any query. |
| `search_synapse_docs` | Find relevant documentation passages — syntax, built-ins, Storm commands. |
| `get_storm_examples` | Pull real clean Storm code examples that match a description. |
| `search_storm_queries` | Find tagged, working queries by what they do (`result_tag`) or how they do it (`technique_tag`). Pass `list_tags=true` to browse available tags. |
| `check_storm_code` | Run the ML lint classifier on a query. Returns label, confidence, and rule code. |
| `get_rule_info` | Get full description, causes, and fix guidance for a specific lint rule code. Pass `"list"` to enumerate all rules. |
| `generate_storm_query` | Generate a query from a natural-language question via RAG + CodeT5. Only available in the `full` binary. |
| `validate_storm_query` | Lex-validate a query string — checks for unrecognised characters and unbalanced brackets. |

---

## Preferred Workflow

### Understanding the data model first
Before writing any Storm, call `get_form_info` with the relevant noun type (e.g. `inet:fqdn`, `risk:threat`, `it:host`). This tells you what properties exist, what can be pivoted through, and what edges connect to other forms. Storm queries that fail at runtime usually do so because a property or form name was wrong.

### Finding documentation context
Use `search_synapse_docs` when you need to understand a Storm feature — subqueries, `yield`, `tee`, `scrape`, HTTP calls, trigger syntax. The vector store covers the full Synapse docs; five results is usually enough.

### Finding examples
Prefer `search_storm_queries` over `get_storm_examples` when you know the shape of what you want to do. The tagged corpus has curated, working queries with intent labels (`ip_enrichment`, `threat_hunting`, `slack_notification`, etc.) and technique labels (`for_loop`, `init_block`, `pivoting`, `http_call`). Use `get_storm_examples` for freeform similarity search against the broader clean corpus.

### Generating a query
`generate_storm_query` runs RAG internally (searches docs + examples) before calling CodeT5, so you do not need to pre-load context. Treat the output as a **draft** — it is typically structurally correct but may have wrong property names or incomplete pivot logic. Always follow up with `validate_storm_query` on the result, then refine against `get_form_info` for property accuracy.

### Validating and linting
- `validate_storm_query` catches syntax-level problems (bad characters, unbalanced `{}`/`()`/`[]`).
- `check_storm_code` catches semantic anti-patterns (variable shadowing, redundant filters, etc.).
- Run both before presenting a final query. If `check_storm_code` returns a violation, call `get_rule_info` with the rule code for remediation guidance.

### Recommended call order for a new query
```
1. get_form_info          — understand the relevant forms
2. search_storm_queries   — find a close existing example
3. search_synapse_docs    — fill in any unfamiliar syntax
4. generate_storm_query   — get a draft (full binary only)
5. validate_storm_query   — confirm syntax
6. check_storm_code       — confirm no lint violations
7. get_rule_info          — fix any flagged violations
```

Steps 1–3 are useful even when writing a query manually, without invoking the generator.

---

## Build Variants

| Binary | ORT | CodeT5 | Use case |
|---|---|---|---|
| `storm-mcp` | embedded | no | Lightweight; all tools available except `generate_storm_query` |
| `storm-mcp-full` | embedded | yes | Full capability; larger binary (~300 MB with models) |
| `storm-mcp-stub` | stubbed | no | Development / protocol testing; no ONNX inference |

The installed binary at `C:\Program Files\StormMCP\storm-mcp-full.exe` is the full variant.
