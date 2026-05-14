package main

import (
	_ "embed"
	"log/slog"
	"os"
)

// ── Embedded assets ────────────────────────────────────────────────────────────

//go:embed synapse_db/vectors.bin
var vectorsBin []byte

//go:embed synapse_db/metadata.json
var metadataJSON []byte

//go:embed model/all-MiniLM-L6-v2.onnx
var embedModelBytes []byte

//go:embed model/tokenizer.json
var tokenizerJSON []byte

//go:embed model/binary_nozip.onnx
var classifierModelBytes []byte

//go:embed corpus/clean_chunks.json
var cleanChunksJSON []byte

//go:embed corpus/tagged_queries.json
var taggedQueriesJSON []byte

// ── Entry point ────────────────────────────────────────────────────────────────

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	log.Info("storm-mcp starting",
		"vectors_bytes", len(vectorsBin),
		"embed_model_bytes", len(embedModelBytes),
		"classifier_bytes", len(classifierModelBytes),
		"chunks_bytes", len(cleanChunksJSON),
		"tagged_queries_bytes", len(taggedQueriesJSON),
	)

	// ── Initialise ORT environment (shared by embedder + CodeT5) ──────────────
	if err := initORTEnv(ortLibBytes); err != nil {
		log.Error("ORT init", "err", err)
		os.Exit(1)
	}
	defer destroyORTEnv()
	log.Info("ORT environment ready")

	// ── Load Synapse doc vector store ──────────────────────────────────────────
	store, err := LoadVectorStore(vectorsBin, metadataJSON)
	if err != nil {
		log.Error("load vector store", "err", err)
		os.Exit(1)
	}
	log.Info("vector store loaded", "chunks", store.Len(), "dim", store.Dim())

	// ── Load embedder (all-MiniLM-L6-v2) ──────────────────────────────────────
	emb, err := NewEmbedder(ortLibBytes, embedModelBytes, tokenizerJSON)
	if err != nil {
		log.Error("load embedder", "err", err)
		os.Exit(1)
	}
	defer emb.Close()
	log.Info("embedder ready", "dim", emb.Dim())

	// ── Load LightGBM classifier ───────────────────────────────────────────────
	clf, err := NewClassifier(classifierModelBytes)
	if err != nil {
		log.Error("load classifier", "err", err)
		os.Exit(1)
	}
	defer clf.Close()
	log.Info("classifier ready")

	// ── Load clean Storm corpus ────────────────────────────────────────────────
	corpus, err := NewCorpusSearcher(cleanChunksJSON)
	if err != nil {
		log.Error("load corpus", "err", err)
		os.Exit(1)
	}
	log.Info("corpus ready", "examples", corpus.Len())

	// ── Load tagged Storm query corpus ─────────────────────────────────────────
	tagged, err := NewTaggedCorpusSearcher(taggedQueriesJSON)
	if err != nil {
		log.Error("load tagged corpus", "err", err)
		os.Exit(1)
	}
	log.Info("tagged corpus ready", "queries", tagged.Len())

	// ── Load CodeT5 Storm query generator (optional — requires embedquery tag) ─
	var queryGen *StormInfer
	if len(encoderGZ) > 0 {
		qg, err := NewStormInfer()
		if err != nil {
			log.Warn("CodeT5 load failed (query generation disabled)", "err", err)
		} else {
			queryGen = qg
			defer queryGen.Close()
			log.Info("CodeT5 query generator ready")
		}
	} else {
		log.Info("CodeT5 not embedded (build without -tags embedquery); query generation disabled")
	}

	// ── Load Synapse forms index ───────────────────────────────────────────────
	formsIdx, err := LoadFormsIndex(formsJSON)
	if err != nil {
		log.Warn("forms index load failed", "err", err)
		formsIdx, _ = LoadFormsIndex(nil)
	}
	log.Info("forms index ready", "forms", formsIdx.Len())

	// ── Start MCP server ───────────────────────────────────────────────────────
	log.Info("MCP server listening on stdio")
	srv := NewMCPServer(store, emb, clf, corpus, tagged, queryGen, formsIdx, os.Stdin, os.Stdout, log)
	if err := srv.Run(); err != nil {
		log.Error("MCP server error", "err", err)
		os.Exit(1)
	}
	log.Info("MCP server shut down")
}
