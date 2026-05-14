//go:build windows && amd64 && !noort

package main

import _ "embed"

//go:embed assets/onnxruntime_windows_amd64.dll
var ortLibBytes []byte

const ortLibExt = ".dll"
