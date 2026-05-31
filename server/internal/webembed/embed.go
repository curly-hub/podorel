package webembed

import "embed"

// Files is the optional embed target for packaged Angular production assets.
//
//go:embed static/*
var Files embed.FS
