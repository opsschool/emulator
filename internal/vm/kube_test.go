package vm

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMachinePod(t *testing.T) {
	k := &Kube{Namespace: "ns", Pod: "m", Image: "reg/{image}:base", OwnerName: "r", OwnerUID: "u", LogHost: "10.1.2.3", MemoryLimit: "4Gi"}
	b, _ := json.Marshal(k.MachinePod("single-node"))
	pod := string(b)
	for _, want := range []string{
		`"image":"reg/single-node:base"`, `"privileged":true`, `"automountServiceAccountToken":false`,
		`{"hostnames":["host.lima.internal"],"ip":"10.1.2.3"}`, `"uid":"u"`, `"limits":{"memory":"4Gi"}`,
		`"hostname":"scenario-vm"`, `"restartPolicy":"Never"`,
	} {
		if !strings.Contains(pod, want) {
			t.Errorf("machine pod lacks %s:\n%s", want, pod)
		}
	}
	if strings.Contains(pod, "runtimeClassName") {
		t.Error("no runtime class was asked for")
	}
}

func TestTarTree(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "checks"), 0o755)
	os.WriteFile(filepath.Join(dir, "checks", "a.sh"), []byte("echo a"), 0o755)
	var buf bytes.Buffer
	if err := tarTree(&buf, dir, "/opt/x/scenario"); err != nil {
		t.Fatal(err)
	}
	var names []string
	tr := tar.NewReader(&buf)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, h.Name)
		if h.Name == "opt/x/scenario/checks/a.sh" {
			if b, _ := io.ReadAll(tr); string(b) != "echo a" || h.Mode&0o111 == 0 {
				t.Errorf("a.sh: %q mode %o", b, h.Mode)
			}
		}
	}
	want := "opt/ opt/x/ opt/x/scenario/ opt/x/scenario/checks/ opt/x/scenario/checks/a.sh"
	if got := strings.Join(names, " "); got != want {
		t.Errorf("entries %q, want %q", got, want)
	}
}
