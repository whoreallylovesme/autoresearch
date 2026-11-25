package artifact

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

var ErrChecksumMismatch = errors.New("artifact checksum mismatch")

const maxArtifactBytes = 64 << 20

type Record struct {
	ID          string    `json:"id"`
	RunID       string    `json:"run_id"`
	Path        string    `json:"path"`
	SHA256      string    `json:"sha256"`
	SizeBytes   int64     `json:"size_bytes"`
	MediaType   string    `json:"media_type"`
	CreatedAt   time.Time `json:"created_at"`
	StoragePath string    `json:"-"`
}

type Store struct {
	root string
}

func New(root string) (*Store, error) {
	if root == "" {
		return nil, errors.New("artifact root is required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &Store{root: root}, nil
}

func (s *Store) Put(ctx context.Context, runID, name, mediaType string, content []byte) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	if len(content) > maxArtifactBytes {
		return Record{}, fmt.Errorf("artifact exceeds %d bytes", maxArtifactBytes)
	}
	digest := sha256.Sum256(content)
	hash := hex.EncodeToString(digest[:])
	directory := filepath.Join(s.root, hash[:2])
	path := filepath.Join(directory, hash)
	name = filepath.Base(name)
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = "artifact"
	}
	record := Record{ID: hash, RunID: runID, Path: name, SHA256: hash, SizeBytes: int64(len(content)), MediaType: mediaType, CreatedAt: time.Now().UTC(), StoragePath: path}
	if _, err := os.Stat(path); err == nil {
		return record, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Record{}, err
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return Record{}, err
	}
	temporary, err := os.CreateTemp(directory, ".upload-*")
	if err != nil {
		return Record{}, err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := writeContext(ctx, temporary, bytes.NewReader(content)); err != nil {
		temporary.Close()
		return Record{}, err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return Record{}, err
	}
	if err := temporary.Close(); err != nil {
		return Record{}, err
	}
	if err := os.Chmod(temporaryPath, 0o400); err != nil {
		return Record{}, err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		if _, statErr := os.Stat(path); statErr == nil {
			return record, nil
		}
		return Record{}, err
	}
	return record, nil
}

func (s *Store) Get(record Record) ([]byte, error) {
	info, err := os.Stat(record.StoragePath)
	if err != nil {
		return nil, err
	}
	if info.Size() != record.SizeBytes {
		return nil, ErrChecksumMismatch
	}
	file, err := os.Open(record.StoragePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxArtifactBytes+1))
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(content)
	if hex.EncodeToString(digest[:]) != record.SHA256 {
		return nil, ErrChecksumMismatch
	}
	return content, nil
}

func writeContext(ctx context.Context, writer io.Writer, reader io.Reader) error {
	buffer := make([]byte, 128*1024)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		read, readErr := reader.Read(buffer)
		if read > 0 {
			if _, err := writer.Write(buffer[:read]); err != nil {
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}
