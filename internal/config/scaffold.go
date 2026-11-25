package config

import (
	"embed"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed scaffold
var scaffoldFiles embed.FS

func Scaffold(directory string) error {
	if directory == "" {
		return errors.New("project directory is required")
	}
	return fs.WalkDir(scaffoldFiles, "scaffold", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel("scaffold", path)
		if err != nil {
			return err
		}
		target := filepath.Join(directory, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		content, err := fs.ReadFile(scaffoldFiles, path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = file.Write(content)
		return err
	})
}
