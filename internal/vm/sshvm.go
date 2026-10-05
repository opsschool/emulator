package vm

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// sshVM is the part of the hosted VM drivers (KubeVirt, EC2) that works
// the machine over SSH: cloud-init gives it the session's key, and the
// runner logs in as vmUser and uses sudo.
type sshVM struct {
	// address returns the machine's address on the runner's network.
	address func(ctx context.Context) (string, error)
	key     string // the session's SSH private key file
}

// vmUser is the account the runner logs in with; cloud-init creates it.
const vmUser = "opsschool"

// newKey makes the session's key pair and returns the public key.
func (s *sshVM) newKey(ctx context.Context) (string, error) {
	dir, err := os.MkdirTemp("", "opsschool-ssh-")
	if err != nil {
		return "", err
	}
	s.key = filepath.Join(dir, "id_ed25519")
	if _, err := must(ctx, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "opsschool-runner", "-f", s.key); err != nil {
		return "", err
	}
	pub, err := os.ReadFile(s.key + ".pub")
	return strings.TrimSpace(string(pub)), err
}

// removeKey deletes the session's key pair.
func (s *sshVM) removeKey() {
	if s.key != "" {
		os.RemoveAll(filepath.Dir(s.key))
	}
}

// cloudConfig is the machine's cloud-init user data: the runner's login,
// and the address Alloy sends logs to, set on every boot. extra is added
// at the end.
func cloudConfig(pubkey, logHost, extra string) string {
	return fmt.Sprintf(`#cloud-config
hostname: scenario-vm
users:
  - name: %s
    sudo: "ALL=(ALL) NOPASSWD:ALL"
    shell: /bin/bash
    lock_passwd: true
    ssh_authorized_keys:
      - %s
bootcmd:
  - sed -i '/host\.lima\.internal/d' /etc/hosts
  - echo '%s host.lima.internal' >>/etc/hosts
%s`, vmUser, strings.TrimSpace(pubkey), logHost, extra)
}

// sshArgs returns an ssh command line to the machine. Each session's VM is
// new, so its host key is too.
func (s *sshVM) sshArgs(ip string, tty bool, remote ...string) []string {
	args := []string{"ssh", "-i", s.key,
		"-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null", "-o", "LogLevel=ERROR",
		"-o", "ConnectTimeout=5", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=4"}
	if tty {
		args = append(args, "-t")
	} else {
		args = append(args, "-T")
	}
	return append(append(args, vmUser+"@"+ip), remote...)
}

func (s *sshVM) ssh(ctx context.Context, stdin io.Reader, remote ...string) (Result, error) {
	ip, err := s.address(ctx)
	if err != nil {
		return Result{}, err
	}
	argv := s.sshArgs(ip, false, remote...)
	return run(ctx, stdin, argv[0], argv[1:]...)
}

func (s *sshVM) Run(ctx context.Context, script string, env []string) (Result, error) {
	return s.ssh(ctx, strings.NewReader(envScript(script, env)), "sudo", "bash", "-s")
}

func (s *sshVM) CopyIn(ctx context.Context, local, remote string) error {
	var buf bytes.Buffer
	if err := tarTree(&buf, local, remote); err != nil {
		return err
	}
	res, err := s.ssh(ctx, &buf, "sudo", "tar", "-x", "--no-overwrite-dir", "-C", "/", "-f", "-")
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("copy into machine: %s", strings.TrimSpace(res.Stderr))
	}
	return nil
}

func (s *sshVM) ShellCommand() []string {
	// The address is stable for the VM's life; look it up now.
	ip, err := s.address(context.Background())
	if err != nil {
		return []string{"sh", "-c", "echo 'The machine is not reachable.'; sleep 5"}
	}
	return append([]string{"env", "TERM=xterm-256color"}, s.sshArgs(ip, true, "sudo", "-i")...)
}

// Reboot restarts the VM's operating system and waits for it to boot again.
func (s *sshVM) Reboot(ctx context.Context) error {
	before, err := s.bootID(ctx)
	if err != nil {
		return err
	}
	// The connection drops as the machine goes down.
	s.Run(ctx, "systemctl reboot", nil)
	deadline := time.Now().Add(5 * time.Minute)
	for {
		if id, err := s.bootID(ctx); err == nil && id != before {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the machine did not come back from its reboot")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	return waitBooted(ctx, s, 5*time.Minute)
}

func (s *sshVM) bootID(ctx context.Context) (string, error) {
	res, err := s.Run(ctx, "cat /proc/sys/kernel/random/boot_id", nil)
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("machine unreachable")
	}
	return strings.TrimSpace(res.Stdout), nil
}
