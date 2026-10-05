// Package images holds what learners are told about each machine image:
// its architecture, and background changes for the Recent changes tab.
// The rest of images/<image>/ builds the image.
package images

import (
	"embed"
	"errors"
	"io/fs"

	"github.com/opsschool/emulator/internal/scenario"
)

//go:embed */architecture.yaml */changes.yaml
var files embed.FS

// Architecture returns an image's architecture.yaml, or nil if it has none.
func Architecture(image string) ([]byte, error) {
	b, err := files.ReadFile(image + "/architecture.yaml")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return b, err
}

// Changes returns an image's background changes.
func Changes(image string) ([]scenario.Change, error) {
	b, err := files.ReadFile(image + "/changes.yaml")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return scenario.ParseChanges(b)
}
