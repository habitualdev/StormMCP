//go:build embedquery

package main

import _ "embed"

// CodeT5-base ONNX models — INT8 quantized, gzip compressed.

//go:embed model/encoder.onnx.gz
var encoderGZ []byte

//go:embed model/decoder.onnx.gz
var decoderGZ []byte

//go:embed model/vocab.json.gz
var vocabGZ []byte

//go:embed model/merges.txt.gz
var mergesGZ []byte

//go:embed model/added_tokens.json
var addedTokensJSON []byte

//go:embed forms.json
var formsJSON []byte
