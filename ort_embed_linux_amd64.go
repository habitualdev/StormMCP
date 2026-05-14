//go:build linux && amd64 && !noort

package main

import _ "embed"

//go:embed assets/onnxruntime_linux_amd64.so
var ortLibBytes []byte

const ortLibExt = ".so"
