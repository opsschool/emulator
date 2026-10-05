package vm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestKubeVirtManifest(t *testing.T) {
	v := &KubeVirt{Namespace: "ns", VMI: "m", Image: "reg/{image}-vm:base", OwnerName: "r", OwnerUID: "u", LogHost: "10.1.2.3", Session: "s1"}
	b, _ := json.Marshal(v.Manifest("single-node", "ssh-ed25519 AAAA runner\n"))
	vmi := string(b)
	for _, want := range []string{
		`"kind":"VirtualMachineInstance"`, `"image":"reg/single-node-vm:base"`, `"uid":"u"`,
		`"opsschool.org/session":"s1"`, `"hostname":"scenario-vm"`, `"macAddress":"` + vmMAC + `"`,
		`ssh-ed25519 AAAA runner\n`, `10.1.2.3 host.lima.internal`,
	} {
		if !strings.Contains(vmi, want) {
			t.Errorf("manifest lacks %s:\n%s", want, vmi)
		}
	}
	if strings.Contains(vmi, `"resources"`) {
		t.Error("no CPU request was asked for")
	}
}
