//go:build !embedquery

package main

// Nil when built without -tags embedquery; NewStormInfer returns nil.
var (
	encoderGZ       []byte
	decoderGZ       []byte
	vocabGZ         []byte
	mergesGZ        []byte
	addedTokensJSON []byte
	formsJSON       []byte
)
