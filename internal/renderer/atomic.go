package renderer

import (
	"fmt"
	"os"
)

// AtomicSwap moves originalDir to oldDir, then renderDir to originalDir, and removes oldDir.
func AtomicSwap(renderDir, originalDir string) error {
	oldDir := originalDir + ".old"
	_ = os.RemoveAll(oldDir)

	if err := os.Rename(originalDir, oldDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to move old directory aside: %w", err)
	}
	if err := os.Rename(renderDir, originalDir); err != nil {
		_ = os.Rename(oldDir, originalDir) // Try to put original back
		return fmt.Errorf("failed to put new directory in place: %w", err)
	}
	_ = os.RemoveAll(oldDir)
	return nil
}

// PrepareStagingDir returns a staging directory path and removes it if it exists.
func PrepareStagingDir(originalDir string) (string, error) {
	renderDir := originalDir + ".new"
	if err := os.RemoveAll(renderDir); err != nil {
		return "", fmt.Errorf("failed to clear staging directory: %w", err)
	}
	return renderDir, nil
}

// SetupStagingDir prepares a temporary staging directory if clean is requested.
// It returns the temporary directory name and a cleanup function to defer.
func SetupStagingDir(clean bool, outputDir string) (renderDir string, cleanup func(), err error) {
	if !clean {
		return outputDir, func() {}, nil
	}

	renderDir, err = PrepareStagingDir(outputDir)
	if err != nil {
		return "", nil, err
	}

	cleanup = func() {
		os.RemoveAll(renderDir)
	}

	return renderDir, cleanup, nil
}
