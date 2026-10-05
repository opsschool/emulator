package session

import (
	"sort"
	"time"

	"github.com/opsschool/emulator/images"
	"github.com/opsschool/emulator/internal/scenario"
)

// ChangeView is a change on the Recent changes tab, with when it went out.
type ChangeView struct {
	scenario.Change
	At time.Time `json:"at"`
}

// changes lists the image's background changes, dated before the session
// started, and once the scenario has begun its own changes, dated before
// the page. Newest first.
func (d *Daemon) changes() []ChangeView {
	d.mu.Lock()
	st := *d.st
	d.mu.Unlock()
	start := time.Now()
	if !st.BaselineEnds.IsZero() {
		start = st.BaselineEnds.Add(-Baseline)
	}
	out := []ChangeView{}
	bg, err := images.Changes(st.Image)
	if err != nil {
		d.Log.Printf("changes: %v", err)
	}
	for _, c := range bg {
		out = append(out, ChangeView{Change: c, At: start.Add(-c.Before.Duration)})
	}
	if st.Begun() {
		for _, c := range d.Scenario.Changes {
			if !c.Applies(st.Vars) {
				continue
			}
			out = append(out, ChangeView{Change: c.Resolve(st.Vars), At: st.StartedAt.Add(-c.Before.Duration)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out
}
