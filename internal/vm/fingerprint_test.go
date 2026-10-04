package vm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFingerprint(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("images/single-node/provision.sh", "echo one")
	write("demoapp/cmd/shop/main.go", "package main")
	fp := func() string {
		f, err := Fingerprint(root, "single-node")
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	base := fp()
	if fp() != base {
		t.Fatal("not stable")
	}
	// Tests, testdata, other images and the rest of the repo don't go into
	// the image.
	write("demoapp/store/store_test.go", "package store")
	write("demoapp/testdata/orders.json", "[]")
	write("images/other/provision.sh", "echo")
	write("scenarios/linux/1.1/break.sh", "echo")
	if fp() != base {
		t.Error("files outside the image changed the fingerprint")
	}
	write("demoapp/store/store.go", "package store")
	next := fp()
	if next == base {
		t.Error("new shop source didn't change the fingerprint")
	}
	write("images/single-node/provision.sh", "echo two")
	if fp() == next {
		t.Error("an edited provision.sh didn't change the fingerprint")
	}
	if _, err := Fingerprint(root, "missing"); err == nil {
		t.Error("a missing image should be an error")
	}
}
