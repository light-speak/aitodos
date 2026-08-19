package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/light-speak/aitodos/internal/project"
)

func writeMetadata(path string, metadata Metadata) error {
	content, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("encode daemon metadata: %w", err)
	}
	content = append(content, '\n')

	temporary, err := os.CreateTemp(filepath.Dir(path), ".daemon-*.json")
	if err != nil {
		return fmt.Errorf("create daemon metadata temp file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace daemon metadata: %w", err)
	}
	return nil
}

func readMetadata(path string) (Metadata, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Metadata{}, err
	}
	var metadata Metadata
	if err := json.Unmarshal(content, &metadata); err != nil {
		return Metadata{}, fmt.Errorf("decode daemon metadata: %w", err)
	}
	return metadata, nil
}

func removeCurrentMetadata(currentProject *project.Project, nonce string) {
	metadata, err := readMetadata(currentProject.Paths.DaemonState)
	if err != nil || metadata.Nonce != nonce {
		return
	}
	_ = os.Remove(currentProject.Paths.DaemonState)
}
