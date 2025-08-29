package renderer

import (
	"io/fs"
	"os"
	"path/filepath"
)

// fsFromDirImpl implements fs.FS for a directory path.
type fsFromDirImpl string

func (dir fsFromDirImpl) Open(name string) (fs.File, error) {
	return os.Open(filepath.Join(string(dir), name))
}
