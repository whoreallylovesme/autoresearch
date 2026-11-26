package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestSQLiteSaveEntityWithEventReturnsEntityError(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "research.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	entityErr := errors.New("save entity")
	err = store.saveEntityWithEvent(context.Background(), func(executor) error {
		return entityErr
	}, nil)
	if !errors.Is(err, entityErr) {
		t.Fatalf("saveEntityWithEvent error = %v, want %v", err, entityErr)
	}
}
