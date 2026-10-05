package vm

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DescriptionFiles describe an image for the session page's Architecture
// and Recent changes tabs.
var DescriptionFiles = map[string]bool{"architecture.yaml": true, "changes.yaml": true}

// Fingerprint identifies what a base image is built from: every file in
// images/<image>/ and the shop's source in demoapp/ (tests aside). A build
// records it, and a session compares it with the checkout's, so a learner
// who pulls changes that need a rebuild is told so. go.mod and go.sum are
// left out: they change with the CLI's dependencies far more often than
// with the shop's. So are the files that describe the image to learners
// (DescriptionFiles): they don't change what's built.
func Fingerprint(root, image string) (string, error) {
	var files []string
	for _, dir := range []string{filepath.Join("images", image), "demoapp"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && d.Name() == "testdata" {
				return filepath.SkipDir
			}
			if d.Type().IsRegular() && !strings.HasSuffix(d.Name(), "_test.go") && !DescriptionFiles[d.Name()] {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	sort.Strings(files)
	h := sha256.New()
	for _, path := range files {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return "", err
		}
		f, err := os.Open(path)
		if err != nil {
			return "", err
		}
		io.WriteString(h, filepath.ToSlash(rel)+"\x00")
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil {
			return "", err
		}
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}
