//go:build wails

package main

import (
	"io/fs"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

func main() {
	assets, err := fs.Sub(frontendAssets, "frontend")
	if err != nil {
		log.Fatal(err)
	}
	app := NewApp()
	err = wails.Run(&options.App{
		Title: "SATSSDV-Go", Width: 1440, Height: 900, MinWidth: 1120, MinHeight: 720,
		AssetServer: &assetserver.Options{Assets: assets},
		OnStartup:   app.startup,
		OnShutdown:  app.shutdown,
		Bind:        []interface{}{app},
	})
	if err != nil {
		log.Fatal(err)
	}
}
