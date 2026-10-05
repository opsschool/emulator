package images

import (
	"io/fs"
	"testing"

	"github.com/opsschool/emulator/internal/webui"
)

// Every image's description parses, and calls name components that exist.
func TestDescriptions(t *testing.T) {
	dirs, _ := fs.ReadDir(files, ".")
	for _, d := range dirs {
		image := d.Name()
		if _, err := Changes(image); err != nil {
			t.Errorf("%s changes: %v", image, err)
		}
		b, err := Architecture(image)
		if err != nil || b == nil {
			t.Errorf("%s architecture: %v", image, err)
			continue
		}
		a, err := webui.ParseArchitecture(b)
		if err != nil {
			t.Errorf("%s architecture: %v", image, err)
			continue
		}
		ids := map[string]bool{}
		for _, z := range a.Zones {
			for _, c := range z.Components {
				if ids[c.ID] {
					t.Errorf("%s: component %s twice", image, c.ID)
				}
				ids[c.ID] = true
			}
		}
		for _, z := range a.Zones {
			for _, c := range z.Components {
				for _, to := range c.Calls {
					if !ids[to] {
						t.Errorf("%s: %s calls unknown component %s", image, c.ID, to)
					}
				}
			}
		}
	}
	if b, err := Architecture("no-such-image"); b != nil || err != nil {
		t.Errorf("missing image: %v %v", b, err)
	}
}
