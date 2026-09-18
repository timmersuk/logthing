package incidents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type FilePersistence struct {
	path string
}

func NewFilePersistence(path string) *FilePersistence {
	return &FilePersistence{path: path}
}

func (p *FilePersistence) Load(ctx context.Context) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	data, err := os.ReadFile(p.path)
	if errors.Is(err, os.ErrNotExist) {
		return Snapshot{}, nil
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("read incident state: %w", err)
	}
	var state Snapshot
	if err := json.Unmarshal(data, &state); err != nil {
		return Snapshot{}, fmt.Errorf("decode incident state: %w", err)
	}
	return state, nil
}

func (p *FilePersistence) Save(ctx context.Context, state Snapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p.path), 0o755); err != nil {
		return fmt.Errorf("create incident state directory: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode incident state: %w", err)
	}
	data = append(data, '\n')

	temporary, err := os.CreateTemp(filepath.Dir(p.path), ".incidents-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary incident state: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("secure temporary incident state: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary incident state: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync temporary incident state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary incident state: %w", err)
	}

	if err := replaceFile(temporaryPath, p.path); err != nil {
		return fmt.Errorf("replace incident state: %w", err)
	}
	return nil
}

func replaceFile(source, target string) error {
	if err := os.Rename(source, target); err == nil {
		return nil
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(source, target)
}
