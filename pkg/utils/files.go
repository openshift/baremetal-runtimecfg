package utils

import (
	"errors"
	"os"
	"path/filepath"
)

// WriteFileAtomically writes data to a temporary file beside path and renames
// it into place only after the write and close succeed.
func WriteFileAtomically(path string, data []byte, mode os.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".runtimecfg-")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		if err := os.Remove(temporaryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.WithField("path", temporaryPath).WithError(err).Warn("Failed to remove temporary file")
		}
	}()

	if err := temporary.Chmod(mode); err != nil {
		return errors.Join(err, temporary.Close())
	}
	if _, err := temporary.Write(data); err != nil {
		return errors.Join(err, temporary.Close())
	}
	if err := temporary.Close(); err != nil {
		return err
	}

	return os.Rename(temporaryPath, path)
}
