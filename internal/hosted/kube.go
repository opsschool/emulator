package hosted

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/opsschool/emulator/internal/telemetry"
)

// Session is one learner's session as the portal sees it.
type Session struct {
	ID       string    `json:"id"`
	User     string    `json:"user"`
	Scenario string    `json:"scenario"`
	Created  time.Time `json:"created"`
	// Phase is the runner pod's: Pending, Running, Succeeded or Failed.
	Phase string `json:"phase"`
	// Waiting is why a pending pod hasn't started, such as an image pull.
	Waiting string `json:"waiting,omitempty"`
	// Error is why a failed runner stopped. It can name the scenario's
	// fault, so it goes to the portal's log, never to learners.
	Error string `json:"-"`
	Token string `json:"-"`
	// Addr is the runner pod's IP, once it has one.
	Addr string `json:"-"`
	// GrafanaRoot is Grafana's public URL, which it needs to serve from
	// the session's path on the portal.
	GrafanaRoot string `json:"-"`
}

// Ended reports whether the session's runner has exited.
func (s Session) Ended() bool { return s.Phase == "Succeeded" || s.Phase == "Failed" }

// Backend creates and finds session pods.
type Backend interface {
	Create(ctx context.Context, s Session) error
	List(ctx context.Context) ([]Session, error)
	Delete(ctx context.Context, id string) error
}

// Kube is the Backend on Kubernetes. It runs kubectl, which finds the
// portal's service account inside the cluster.
type Kube struct {
	Namespace string
	// Image is the opsschool image, for the runner and its init container.
	Image string
	// MachineImage is the scenario machine image; "{image}" is replaced
	// with the scenario's image name.
	MachineImage string
	// PortalURL is the portal's address inside the cluster, where runners
	// send results.
	PortalURL string
	// ServiceAccount runs the session pods. It needs to create, read and
	// delete pods, and exec into them.
	ServiceAccount string
	// MachineDriver is how runners make machines: "kubevirt" for a
	// KubeVirt VM (vm.KubeVirt), "ec2" for an EC2 instance (vm.EC2) or
	// "kubernetes" for a privileged pod (vm.Kube).
	MachineDriver string
	// Machine settings; see vm.Kube and vm.KubeVirt.
	RuntimeClass, MachineCPU, MachineMemory, MachineMemoryLimit string
	// MachineEnv is more environment for the runner's machine driver, such
	// as the EC2 driver's settings.
	MachineEnv map[string]string
}

const (
	labelComponent = "app.kubernetes.io/component"
	labelSession   = "opsschool.org/session"
	annUser        = "opsschool.org/user"
	annScenario    = "opsschool.org/scenario"
	annToken       = "opsschool.org/token"
	annGrafana     = "opsschool.org/grafana-root"
)

// RunnerPodName and MachinePodName name a session's pods.
func RunnerPodName(id string) string  { return "opsschool-session-" + id }
func MachinePodName(id string) string { return "opsschool-machine-" + id }

func (k *Kube) kubectl(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "kubectl", append([]string{"--namespace", k.Namespace}, args...)...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var errb bytes.Buffer
	cmd.Stderr = &errb
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("kubectl %s: %v: %s", args[0], err, strings.TrimSpace(errb.String()))
	}
	return out, nil
}

func (k *Kube) Create(ctx context.Context, s Session) error {
	b, err := json.Marshal(k.RunnerPod(s))
	if err != nil {
		return err
	}
	_, err = k.kubectl(ctx, b, "create", "-f", "-")
	return err
}

func (k *Kube) Delete(ctx context.Context, id string) error {
	// The machine pod is owned by the runner pod and goes with it.
	_, err := k.kubectl(ctx, nil, "delete", "pod", RunnerPodName(id), "--ignore-not-found", "--wait=false")
	return err
}

type podList struct {
	Items []struct {
		Metadata struct {
			Labels            map[string]string `json:"labels"`
			Annotations       map[string]string `json:"annotations"`
			CreationTimestamp time.Time         `json:"creationTimestamp"`
		} `json:"metadata"`
		Status struct {
			Phase             string `json:"phase"`
			PodIP             string `json:"podIP"`
			ContainerStatuses []struct {
				Name  string `json:"name"`
				State struct {
					Terminated *struct {
						ExitCode int    `json:"exitCode"`
						Message  string `json:"message"`
					} `json:"terminated"`
					Waiting *struct {
						Reason string `json:"reason"`
					} `json:"waiting"`
				} `json:"state"`
			} `json:"containerStatuses"`
		} `json:"status"`
	} `json:"items"`
}

func (k *Kube) List(ctx context.Context) ([]Session, error) {
	out, err := k.kubectl(ctx, nil, "get", "pods", "-l", labelComponent+"=session", "-o", "json")
	if err != nil {
		return nil, err
	}
	return parsePods(out)
}

func parsePods(b []byte) ([]Session, error) {
	var pl podList
	if err := json.Unmarshal(b, &pl); err != nil {
		return nil, err
	}
	var out []Session
	for _, p := range pl.Items {
		m := p.Metadata
		s := Session{
			ID: m.Labels[labelSession], User: m.Annotations[annUser], Scenario: m.Annotations[annScenario],
			Token: m.Annotations[annToken], GrafanaRoot: m.Annotations[annGrafana],
			Created: m.CreationTimestamp, Phase: p.Status.Phase, Addr: p.Status.PodIP,
		}
		for _, c := range p.Status.ContainerStatuses {
			if w := c.State.Waiting; w != nil && w.Reason != "" && w.Reason != "PodInitializing" {
				s.Waiting = w.Reason
			}
			// On clusters without native sidecars, the telemetry keeps the
			// pod running after the runner exits.
			if t := c.State.Terminated; c.Name == "runner" && t != nil {
				s.Phase = "Succeeded"
				if t.ExitCode != 0 {
					s.Phase = "Failed"
					s.Error = strings.TrimSpace(t.Message)
				}
			}
		}
		if s.ID != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

// RunnerPod is a session's pod: the runner, which plays `opsschool start`
// and the session daemon, and the telemetry stack beside it. Everything
// in it shares the pod's loopback, as the CLI and the stack share a
// laptop's, and the runner forwards the VM's usual ports to the machine,
// as Lima does.
func (k *Kube) RunnerPod(s Session) map[string]any {
	field := func(name, path string) map[string]any {
		return map[string]any{"name": name, "valueFrom": map[string]any{"fieldRef": map[string]any{"fieldPath": path}}}
	}
	val := func(name, v string) map[string]any { return map[string]any{"name": name, "value": v} }
	env := []any{
		field("OPSSCHOOL_NAMESPACE", "metadata.namespace"),
		field("OPSSCHOOL_POD_NAME", "metadata.name"),
		field("OPSSCHOOL_POD_UID", "metadata.uid"),
		field("OPSSCHOOL_POD_IP", "status.podIP"),
		val("OPSSCHOOL_HOME", "/var/lib/opsschool"),
		val("OPSSCHOOL_SESSION", s.ID),
		val("OPSSCHOOL_SCENARIO", s.Scenario),
		val("OPSSCHOOL_USER", s.User),
		val("OPSSCHOOL_TOKEN", s.Token),
		val("OPSSCHOOL_PORTAL_URL", k.PortalURL),
		val("OPSSCHOOL_PREFIX", "/s/"+s.ID+"/"),
		val("OPSSCHOOL_MACHINE_POD", MachinePodName(s.ID)),
		val("OPSSCHOOL_MACHINE_IMAGE", k.MachineImage),
	}
	for name, v := range map[string]string{
		"OPSSCHOOL_MACHINE_DRIVER":       k.MachineDriver,
		"OPSSCHOOL_RUNTIME_CLASS":        k.RuntimeClass,
		"OPSSCHOOL_MACHINE_CPU":          k.MachineCPU,
		"OPSSCHOOL_MACHINE_MEMORY":       k.MachineMemory,
		"OPSSCHOOL_MACHINE_MEMORY_LIMIT": k.MachineMemoryLimit,
	} {
		if v != "" {
			env = append(env, val(name, v))
		}
	}
	for name, v := range k.MachineEnv {
		env = append(env, val(name, v))
	}
	mount := func(name, path string, sub ...string) map[string]any {
		m := map[string]any{"name": name, "mountPath": path}
		if len(sub) > 0 {
			m["subPath"] = sub[0]
			m["readOnly"] = true
		}
		return m
	}
	res := func(cpu, mem string) map[string]any {
		return map[string]any{"requests": map[string]string{"cpu": cpu, "memory": mem}}
	}
	tv := telemetry.Versions
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]any{
			"name": RunnerPodName(s.ID),
			"labels": map[string]string{
				"app.kubernetes.io/name": "opsschool", labelComponent: "session", labelSession: s.ID,
			},
			"annotations": map[string]string{
				annUser: s.User, annScenario: s.Scenario, annToken: s.Token, annGrafana: s.GrafanaRoot,
			},
		},
		"spec": map[string]any{
			"serviceAccountName":            k.ServiceAccount,
			"restartPolicy":                 "Never",
			"enableServiceLinks":            false,
			"terminationGracePeriodSeconds": 30,
			"initContainers": []any{
				map[string]any{
					"name":         "telemetry-config",
					"image":        k.Image,
					"command":      []string{"opsschool", "_render-telemetry", s.Scenario, "/telemetry"},
					"volumeMounts": []any{mount("telemetry", "/telemetry")},
				},
				// Telemetry runs as sidecars, so the pod completes when the
				// runner exits (Kubernetes 1.29 and later).
				map[string]any{
					"name": "prometheus", "restartPolicy": "Always", "image": "prom/prometheus:v" + tv.Prometheus,
					"args": []string{
						"--config.file=/telemetry/prometheus.yml", "--storage.tsdb.path=/prometheus",
						"--storage.tsdb.retention.time=1d",
						fmt.Sprintf("--web.listen-address=127.0.0.1:%d", telemetry.PrometheusPort),
					},
					"resources":    res("100m", "256Mi"),
					"volumeMounts": []any{mount("telemetry", "/telemetry"), mount("prometheus", "/prometheus")},
				},
				map[string]any{
					// Alloy on the machine pushes logs here.
					"name": "loki", "restartPolicy": "Always", "image": "grafana/loki:" + tv.Loki,
					"args": []string{
						"-config.file=/telemetry/loki.yaml", "-server.http-listen-address=0.0.0.0",
						fmt.Sprintf("-server.http-listen-port=%d", telemetry.LokiPort),
					},
					"ports":        []any{map[string]any{"name": "loki", "containerPort": telemetry.LokiPort}},
					"resources":    res("100m", "256Mi"),
					"volumeMounts": []any{mount("telemetry", "/telemetry"), mount("loki", "/tmp/loki")},
				},
				map[string]any{
					"name": "grafana", "restartPolicy": "Always", "image": "grafana/grafana:" + tv.Grafana,
					"env": []any{
						val("GF_SERVER_HTTP_ADDR", "0.0.0.0"),
						val("GF_SERVER_HTTP_PORT", fmt.Sprint(telemetry.GrafanaPort)),
						val("GF_SERVER_ROOT_URL", s.GrafanaRoot),
						val("GF_SERVER_SERVE_FROM_SUB_PATH", "true"),
						val("GF_AUTH_ANONYMOUS_ENABLED", "true"),
						val("GF_AUTH_ANONYMOUS_ORG_ROLE", "Admin"),
						val("GF_AUTH_DISABLE_LOGIN_FORM", "true"),
						val("GF_DASHBOARDS_DEFAULT_HOME_DASHBOARD_PATH", "/var/lib/grafana/dashboards/scenario.json"),
						val("GF_DASHBOARDS_MIN_REFRESH_INTERVAL", "5s"),
						val("GF_ANALYTICS_REPORTING_ENABLED", "false"),
						val("GF_NEWS_NEWS_FEED_ENABLED", "false"),
					},
					"ports":     []any{map[string]any{"name": "grafana", "containerPort": telemetry.GrafanaPort}},
					"resources": res("100m", "256Mi"),
					"volumeMounts": []any{
						mount("telemetry", "/etc/grafana/provisioning", "grafana/provisioning"),
						mount("telemetry", "/var/lib/grafana/dashboards", "grafana/dashboards"),
					},
				},
			},
			"containers": []any{
				map[string]any{
					"name": "runner", "image": k.Image,
					"command":      []string{"opsschool", "_runner"},
					"env":          env,
					"ports":        []any{map[string]any{"name": "session", "containerPort": RunnerPort}},
					"resources":    res("250m", "256Mi"),
					"volumeMounts": []any{mount("home", "/var/lib/opsschool")},
				},
			},
			"volumes": []any{
				map[string]any{"name": "telemetry", "emptyDir": map[string]any{}},
				map[string]any{"name": "home", "emptyDir": map[string]any{}},
				map[string]any{"name": "prometheus", "emptyDir": map[string]any{}},
				map[string]any{"name": "loki", "emptyDir": map[string]any{}},
			},
		},
	}
}

// errNoSession is returned for a session that doesn't exist or isn't the
// learner's.
var errNoSession = errors.New("no such session")
