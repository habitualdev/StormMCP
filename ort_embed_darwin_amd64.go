//go:build darwin && amd64 && !noort

package main

import _ "embed"

//go:embed assets/onnxruntime_darwin_amd64.dylib
var ortLibBytes []byte

const ortLibExt = ".dylib"
