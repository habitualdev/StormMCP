//go:build linux && arm64 && !noort

package main

import _ "embed"

//go:embed assets/onnxruntime_linux_arm64.so
var ortLibBytes []byte

const ortLibExt = ".so"
