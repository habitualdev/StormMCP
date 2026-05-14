//go:build darwin && arm64 && !noort

package main

import _ "embed"

//go:embed assets/onnxruntime_darwin_arm64.dylib
var ortLibBytes []byte

const ortLibExt = ".dylib"
