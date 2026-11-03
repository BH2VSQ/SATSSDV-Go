//go:build wails

package main

import "embed"

//go:embed frontend/*
var frontendAssets embed.FS
