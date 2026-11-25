package literature

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type PDFProcessor struct {
	Command string
	Timeout time.Duration
}

func (p PDFProcessor) Extract(ctx context.Context, content []byte) ([]byte, error) {
	if p.Command == "" {
		p.Command = "pdftotext"
	}
	if p.Timeout <= 0 {
		p.Timeout = 30 * time.Second
	}
	if len(content) > 32<<20 {
		return nil, errors.New("PDF exceeds 32 MiB")
	}
	directory, err := os.MkdirTemp("", "autoresearch-pdf-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(directory)
	path := filepath.Join(directory, "paper.pdf")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return nil, err
	}
	extractContext, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()
	command := exec.CommandContext(extractContext, p.Command, "-layout", path, "-")
	command.Dir = directory
	command.Env = []string{"PATH=/usr/bin:/bin"}
	var stdout cappedBuffer
	stdout.limit = 16 << 20
	var stderr cappedBuffer
	stderr.limit = 4096
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if extractContext.Err() != nil {
			return nil, extractContext.Err()
		}
		return nil, fmt.Errorf("PDF text extraction failed: %w", err)
	}
	if stdout.overflow || stderr.overflow {
		return nil, errors.New("PDF extraction output exceeded configured limit")
	}
	return stdout.Bytes(), nil
}

type cappedBuffer struct {
	buffer   bytes.Buffer
	limit    int
	overflow bool
}

func (b *cappedBuffer) Write(content []byte) (int, error) {
	remaining := b.limit - b.buffer.Len()
	if remaining <= 0 {
		b.overflow = true
		return len(content), nil
	}
	if len(content) > remaining {
		b.buffer.Write(content[:remaining])
		b.overflow = true
		return len(content), nil
	}
	return b.buffer.Write(content)
}

func (b *cappedBuffer) Bytes() []byte { return b.buffer.Bytes() }
