package session

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/opsschool/simulator/internal/checks"
	"github.com/opsschool/simulator/internal/loadgen"
	"github.com/opsschool/simulator/internal/scenario"
	"github.com/opsschool/simulator/internal/vm"
)

// VerifyReport is the outcome of fix verification.
type VerifyReport struct {
	At       time.Time     `json:"at"`
	Pass     bool          `json:"pass"`
	DataLoss bool          `json:"data_loss"`
	Failures []string      `json:"failures,omitempty"`
	Duration time.Duration `json:"duration"`
}

// Verifier runs fix verification: restart the scenario's units, reboot if
// required, replay load at peak, then evaluate the fixed tier. The fixed
// tier passes when the fixed checks, the mitigated checks (instantaneous,
// no hold) and the preserve checks all pass.
type Verifier struct {
	Scenario *scenario.Scenario
	Machine  vm.Driver
	Engine   *checks.Engine
	// Load is the running generator. Its profile is switched to peak for
	// the replay and restored afterwards.
	Load *loadgen.Generator
	// Settle is how long to wait after the replay before evaluating, so
	// rate() windows cover post-replay traffic. Default 15s.
	Settle time.Duration
}

// Run performs verification, reporting progress through say.
func (v *Verifier) Run(ctx context.Context, say func(string)) (VerifyReport, error) {
	start := time.Now()
	fv := v.Scenario.Spec.FixVerification
	rep := VerifyReport{At: start}

	if len(fv.Restart) > 0 {
		say("Restarting " + strings.Join(fv.Restart, ", "))
		script := "systemctl restart " + strings.Join(fv.Restart, " ")
		res, err := v.Machine.Run(ctx, script, nil)
		if err != nil {
			return rep, err
		}
		if res.ExitCode != 0 {
			rep.Failures = append(rep.Failures, "restart failed: "+strings.TrimSpace(res.Stderr))
		}
	}
	if fv.Reboot {
		say("Rebooting the machine")
		if err := v.Machine.Reboot(ctx); err != nil {
			rep.Failures = append(rep.Failures, "reboot: "+err.Error())
		}
	}
	if d := fv.LoadReplay.Duration; d > 0 && v.Load != nil {
		say(fmt.Sprintf("Replaying peak load for %s", d))
		prev := v.Load.CurrentProfile()
		v.Load.SetProfile(loadgen.AtPeak(prev))
		err := sleep(ctx, d)
		v.Load.SetProfile(prev)
		if err != nil {
			return rep, err
		}
	}
	settle := v.Settle
	if settle == 0 {
		settle = 15 * time.Second
	}
	if err := sleep(ctx, settle); err != nil {
		return rep, err
	}

	say("Evaluating checks")
	groups := []struct {
		name string
		cs   []scenario.Check
	}{
		{"fixed", v.Scenario.Checks.Fixed},
		{"mitigated", v.Scenario.Checks.Mitigated},
		{"preserve", v.Scenario.Checks.Preserve},
	}
	for _, g := range groups {
		_, outs := v.Engine.EvaluateAll(ctx, g.cs, checks.PhaseCheck)
		for _, o := range outs {
			if !o.Pass {
				rep.Failures = append(rep.Failures, fmt.Sprintf("%s: %s (%s)", g.name, o.Check.Label(), o.Detail))
				if g.name == "preserve" {
					rep.DataLoss = true
				}
			}
		}
	}
	rep.Pass = len(rep.Failures) == 0
	rep.Duration = time.Since(start).Truncate(time.Second)
	return rep, nil
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
