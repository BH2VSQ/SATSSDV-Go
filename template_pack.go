package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BH2VSQ/SATSSDV-Go/internal/gallery"
	"github.com/BH2VSQ/SATSSDV-Go/internal/storage"
	templatepkg "github.com/BH2VSQ/SATSSDV-Go/internal/template"
)

const (
	templatePackFormat  = "satssdv-go-template-pack"
	templatePackVersion = 1
	maxPackAssetBytes   = 32 * 1024 * 1024
	maxPackTotalBytes   = 64 * 1024 * 1024
	maxPackFileBytes    = 96 * 1024 * 1024
)

type templatePackAsset struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Format string `json:"format"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	Data   string `json:"data"`
}

type templatePack struct {
	Format            string                 `json:"format"`
	Version           int                    `json:"version"`
	CreatedAt         time.Time              `json:"createdAt"`
	Templates         []templatepkg.Template `json:"templates"`
	BackgroundAssetID string                 `json:"backgroundAssetId,omitempty"`
	Assets            []templatePackAsset    `json:"assets"`
}

// ExportTemplatePackTo writes a portable preset bundle to a path selected by
// the native Qt file dialog. Embedded assets keep the bundle self-contained.
func (a *App) ExportTemplatePackTo(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("template pack destination path is empty")
	}
	templates, err := a.GetTemplates()
	if err != nil {
		return "", err
	}
	galleryAssets, err := a.GetGallery()
	if err != nil {
		return "", err
	}
	settings := a.GetSettings()
	needed := map[string]bool{}
	for _, item := range templates {
		for _, component := range item.Components {
			if component.Type == "image" && component.AssetID != "" {
				needed[component.AssetID] = true
			}
		}
	}
	if settings.TemplateBackgroundAssetID != "" {
		needed[settings.TemplateBackgroundAssetID] = true
	}
	assetByID := make(map[string]gallery.Asset, len(galleryAssets))
	for _, asset := range galleryAssets {
		assetByID[asset.ID] = asset
	}
	dataDir, err := a.requireDataDir()
	if err != nil {
		return "", err
	}
	galleryRoot, err := filepath.Abs(filepath.Join(dataDir, "gallery", "files"))
	if err != nil {
		return "", err
	}
	pack := templatePack{Format: templatePackFormat, Version: templatePackVersion, CreatedAt: time.Now().UTC(), Templates: templates, BackgroundAssetID: settings.TemplateBackgroundAssetID}
	var total int64
	assetIDs := make([]string, 0, len(needed))
	for id := range needed {
		assetIDs = append(assetIDs, id)
	}
	sort.Strings(assetIDs)
	for _, id := range assetIDs {
		asset, ok := assetByID[id]
		if !ok {
			return "", fmt.Errorf("template references missing gallery asset %q", id)
		}
		path, err := filepath.Abs(asset.Path)
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(galleryRoot, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return "", fmt.Errorf("gallery asset %q is outside the gallery storage directory", id)
		}
		info, err := os.Stat(path)
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maxPackAssetBytes {
			return "", fmt.Errorf("gallery asset %q is invalid or larger than 32 MiB", id)
		}
		total += info.Size()
		if total > maxPackTotalBytes {
			return "", errors.New("template pack assets exceed the 64 MiB total limit")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		pack.Assets = append(pack.Assets, templatePackAsset{
			ID: asset.ID, Name: asset.Name, Format: strings.TrimPrefix(strings.ToLower(filepath.Ext(asset.Path)), "."),
			Width: asset.Width, Height: asset.Height, Data: base64.StdEncoding.EncodeToString(data),
		})
	}
	encoded, err := json.MarshalIndent(pack, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// ImportTemplatePackFrom validates a bundle selected by a native Qt file
// dialog, imports its assets, remaps IDs and appends presets without replacing
// the user's existing templates.
func (a *App) ImportTemplatePackFrom(path string) (int, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return 0, errors.New("template pack source path is empty")
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxPackFileBytes {
		return 0, errors.New("template pack must be a regular file no larger than 96 MiB")
	}
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	encoded, readErr := io.ReadAll(io.LimitReader(file, maxPackFileBytes+1))
	closeErr := file.Close()
	if readErr != nil {
		return 0, readErr
	}
	if closeErr != nil {
		return 0, closeErr
	}
	var pack templatePack
	if err := json.Unmarshal(encoded, &pack); err != nil {
		return 0, fmt.Errorf("read template pack: %w", err)
	}
	if pack.Format != templatePackFormat || pack.Version != templatePackVersion {
		return 0, errors.New("unsupported or invalid SATSSDV-Go template pack format/version")
	}
	if err := templatepkg.Validate(pack.Templates); err != nil {
		return 0, fmt.Errorf("template pack contains invalid presets: %w", err)
	}
	existing, err := a.GetTemplates()
	if err != nil {
		return 0, err
	}
	if len(existing)+len(pack.Templates) > 32 {
		return 0, fmt.Errorf("import would exceed the 32-preset limit (currently %d, pack contains %d)", len(existing), len(pack.Templates))
	}

	assetData := make(map[string][]byte, len(pack.Assets))
	var total int64
	for _, asset := range pack.Assets {
		if strings.TrimSpace(asset.ID) == "" || assetData[asset.ID] != nil {
			return 0, errors.New("template pack contains an empty or duplicate asset ID")
		}
		assetName := filepath.Base(strings.ReplaceAll(strings.TrimSpace(asset.Name), "\\", "/"))
		if assetName == "" || assetName == "." || assetName == string(filepath.Separator) {
			return 0, fmt.Errorf("template pack asset %q has an invalid display name", asset.ID)
		}
		if asset.Format != "png" && asset.Format != "jpg" && asset.Format != "jpeg" && asset.Format != "gif" {
			return 0, fmt.Errorf("template pack asset %q has unsupported format %q", asset.ID, asset.Format)
		}
		data, err := base64.StdEncoding.DecodeString(asset.Data)
		if err != nil || len(data) == 0 || len(data) > maxPackAssetBytes {
			return 0, fmt.Errorf("template pack asset %q has invalid or oversized data", asset.ID)
		}
		total += int64(len(data))
		if total > maxPackTotalBytes {
			return 0, errors.New("template pack assets exceed the 64 MiB total limit")
		}
		config, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || (format != "png" && format != "jpeg" && format != "gif") || config.Width <= 0 || config.Height <= 0 || config.Width > 8192 || config.Height > 8192 || int64(config.Width)*int64(config.Height) > 32*1024*1024 {
			return 0, fmt.Errorf("template pack asset %q is not a supported image or exceeds the image dimension limit", asset.ID)
		}
		assetData[asset.ID] = data
	}
	for _, item := range pack.Templates {
		for _, component := range item.Components {
			if component.Type == "image" {
				if _, ok := assetData[component.AssetID]; !ok {
					return 0, fmt.Errorf("template %q references asset %q that is not in the pack", item.Name, component.AssetID)
				}
			}
		}
	}
	if pack.BackgroundAssetID != "" {
		if _, ok := assetData[pack.BackgroundAssetID]; !ok {
			return 0, fmt.Errorf("template pack background asset %q is missing", pack.BackgroundAssetID)
		}
	}

	imported := make([]gallery.Asset, 0, len(pack.Assets))
	assetIDs := make(map[string]string, len(pack.Assets))
	localIDs := make(map[string]bool, len(pack.Assets))
	rollback := func() {
		dataDir, dataErr := a.requireDataDir()
		if dataErr != nil {
			return
		}
		store := gallery.Store{Root: filepath.Join(dataDir, "gallery", "files")}
		for _, asset := range imported {
			_ = store.Delete(asset)
		}
		current, _ := a.GetGallery()
		keep := current[:0]
		for _, item := range current {
			if !localIDs[item.ID] {
				keep = append(keep, item)
			}
		}
		_ = storage.SaveJSON(filepath.Join(dataDir, "gallery", "index.json"), gallery.Index{Assets: keep})
	}
	for _, asset := range pack.Assets {
		data := assetData[asset.ID]
		ext := "." + asset.Format
		temp, err := os.CreateTemp("", "satssdv-template-*"+ext)
		if err != nil {
			rollback()
			return 0, err
		}
		_, writeErr := temp.Write(data)
		closeErr := temp.Close()
		if writeErr != nil || closeErr != nil {
			_ = os.Remove(temp.Name())
			rollback()
			if writeErr != nil {
				return 0, writeErr
			}
			return 0, closeErr
		}
		local, importErr := a.ImportGalleryImage(temp.Name())
		_ = os.Remove(temp.Name())
		if importErr != nil {
			rollback()
			return 0, importErr
		}
		local.Name = filepath.Base(strings.ReplaceAll(strings.TrimSpace(asset.Name), "\\", "/"))
		// Persist the portable display name while retaining the newly generated
		// local asset ID and path.
		galleryItems, _ := a.GetGallery()
		for i := range galleryItems {
			if galleryItems[i].ID == local.ID {
				galleryItems[i].Name = local.Name
			}
		}
		dataDir, _ := a.requireDataDir()
		if err := storage.SaveJSON(filepath.Join(dataDir, "gallery", "index.json"), gallery.Index{Assets: galleryItems}); err != nil {
			imported = append(imported, local)
			localIDs[local.ID] = true
			rollback()
			return 0, err
		}
		imported = append(imported, local)
		localIDs[local.ID] = true
		assetIDs[asset.ID] = local.ID
	}

	stamp := time.Now().UnixNano()
	newTemplates := make([]templatepkg.Template, len(pack.Templates))
	for i, item := range pack.Templates {
		item.ID = fmt.Sprintf("import-%x-%d", stamp, i+1)
		item.BackgroundAssetID = ""
		for j := range item.Components {
			if item.Components[j].Type == "image" {
				item.Components[j].AssetID = assetIDs[item.Components[j].AssetID]
			}
		}
		if item.UpdatedAt == "" {
			item.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		}
		newTemplates[i] = item
	}
	combined := append(append([]templatepkg.Template(nil), existing...), newTemplates...)
	if err := a.SaveTemplates(combined); err != nil {
		rollback()
		return 0, err
	}
	if pack.BackgroundAssetID != "" {
		settings := a.GetSettings()
		settings.TemplateBackgroundAssetID = assetIDs[pack.BackgroundAssetID]
		if err := a.SaveSettings(settings); err != nil {
			_ = a.SaveTemplates(existing)
			rollback()
			return 0, err
		}
	}
	return len(newTemplates), nil
}

// ExportTemplatePack is retained as a migration hint for older clients.
func (a *App) ExportTemplatePack() (string, error) {
	return "", errors.New("use ExportTemplatePackTo with a path selected by the native file dialog")
}

// ImportTemplatePack is retained as a migration hint for older clients.
func (a *App) ImportTemplatePack() (int, error) {
	return 0, errors.New("use ImportTemplatePackFrom with a path selected by the native file dialog")
}
