package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BH2VSQ/SATSSDV-Go/internal/album"
)

// ExportAlbumRecordToDirectory exports one record into a directory selected
// by the native Qt file dialog.
func (a *App) ExportAlbumRecordToDirectory(id, destination string) (int, error) {
	records, err := a.GetAlbum()
	if err != nil {
		return 0, err
	}
	var selected *album.Record
	for i := range records {
		if records[i].ID == id {
			copy := records[i]
			selected = &copy
			break
		}
	}
	if selected == nil {
		return 0, errors.New("album record not found")
	}
	return a.exportAlbumRecords(destination, []album.Record{*selected})
}

// ExportAlbumToDirectoryPath exports every available album file into a
// directory selected by the native Qt file dialog.
func (a *App) ExportAlbumToDirectoryPath(destination string) (int, error) {
	records, err := a.GetAlbum()
	if err != nil {
		return 0, err
	}
	return a.exportAlbumRecords(destination, records)
}

func (a *App) ExportAlbumRecord(id string) (int, error) {
	return 0, errors.New("use ExportAlbumRecordToDirectory with a path selected by the native directory dialog")
}

func (a *App) ExportAlbumToDirectory() (int, error) {
	return 0, errors.New("use ExportAlbumToDirectoryPath with a path selected by the native directory dialog")
}

func (a *App) exportAlbumRecords(destination string, records []album.Record) (int, error) {
	dataDir, err := a.requireDataDir()
	if err != nil {
		return 0, err
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(destination)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return 0, errors.New("selected export destination is not a directory")
	}
	root, err := filepath.Abs(filepath.Join(dataDir, "album"))
	if err != nil {
		return 0, err
	}
	exported := 0
	for _, record := range records {
		for _, item := range []struct {
			path string
			ext  string
		}{{record.ImagePath, ".png"}, {record.PacketPath, ".ssdv"}} {
			if item.path == "" {
				continue
			}
			source, err := filepath.Abs(item.path)
			if err != nil {
				return exported, err
			}
			rel, err := filepath.Rel(root, source)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
				return exported, fmt.Errorf("album record %q contains a file path outside the album directory", record.ID)
			}
			fileInfo, err := os.Stat(source)
			if err != nil {
				return exported, err
			}
			if !fileInfo.Mode().IsRegular() || fileInfo.Size() <= 0 || fileInfo.Size() > 32*1024*1024 {
				return exported, fmt.Errorf("album file for record %q is invalid or exceeds 32 MiB", record.ID)
			}
			timestamp := record.CreatedAt.UTC().Format("20060102T150405.000000000Z")
			if record.CreatedAt.IsZero() {
				timestamp = time.Now().UTC().Format("20060102T150405.000000000Z")
			}
			name := fmt.Sprintf("%s_%s_%s_%s%s", safeAlbumName(record.Type), timestamp, safeAlbumName(record.Callsign), safeAlbumName(record.ID), item.ext)
			input, err := os.Open(source)
			if err != nil {
				return exported, err
			}
			output, target, err := createUniqueAlbumExportFile(destination, name)
			if err != nil {
				_ = input.Close()
				return exported, fmt.Errorf("create export file %q: %w", name, err)
			}
			_, copyErr := io.Copy(output, input)
			inputErr := input.Close()
			outputErr := output.Close()
			if copyErr != nil || inputErr != nil || outputErr != nil {
				_ = os.Remove(target)
				if copyErr != nil {
					return exported, copyErr
				}
				if inputErr != nil {
					return exported, inputErr
				}
				return exported, outputErr
			}
			exported++
		}
	}
	return exported, nil
}

func createUniqueAlbumExportFile(destination, name string) (*os.File, string, error) {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for index := 0; index < 10000; index++ {
		candidate := name
		if index > 0 {
			candidate = fmt.Sprintf("%s_%d%s", stem, index, ext)
		}
		path := filepath.Join(destination, candidate)
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			return file, path, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, "", err
		}
	}
	return nil, "", errors.New("too many files with the same export name in the selected folder")
}
