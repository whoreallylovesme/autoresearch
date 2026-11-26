package executor

import (
	"context"
	"errors"

	"github.com/whoreallylovesme/autoresearch/internal/artifact"
)

const (
	maxArtifactCount      = 20
	maxArtifactFileBytes  = 16 << 20
	maxArtifactTotalBytes = 64 << 20
)

type artifactCollection struct {
	store   *artifact.Store
	runID   string
	records []artifact.Record
	total   int64
}

func (c *artifactCollection) put(ctx context.Context, name string, size int64, read func() ([]byte, error)) error {
	if len(c.records) >= maxArtifactCount {
		return errors.New("too many artifacts")
	}
	if size > maxArtifactFileBytes || c.total+size > maxArtifactTotalBytes {
		return errors.New("artifact size limit exceeded")
	}
	content, err := read()
	if err != nil {
		return err
	}
	record, err := c.store.Put(ctx, c.runID, name, "application/octet-stream", content)
	if err != nil {
		return err
	}
	c.total += size
	c.records = append(c.records, record)
	return nil
}
