package vm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

// AMIOptions places the instance BuildAMI provisions. The machine running
// the build must reach it over SSH: give it a subnet that assigns public
// addresses and a security group that lets you in on port 22, or run the
// build from inside the VPC.
type AMIOptions struct {
	Subnet         string
	SecurityGroups []string
	InstanceType   string
}

// ubuntuAMIParam is where Canonical publishes the current Ubuntu 26.04
// AMI, as the Lima template uses the current cloud image.
const ubuntuAMIParam = "/aws/service/canonical/ubuntu/server/26.04/stable/current/amd64/hvm/ebs-gp3/ami-id"

// BuildAMI builds the image as an AMI, for the ec2 driver. It does on EC2
// what images/<image>/build.sh does with Lima: boots Ubuntu, provisions it
// with the image's provision.sh and checks it with smoke.sh. Then it names
// the network card eth0, as Lima does, resets the machine with
// images/vm-export.sh and saves the disk as an AMI tagged with the
// fingerprint. Building on EC2 rather than importing the Lima disk keeps
// the drivers the instance needs (NVMe, ENA) in its initrd.
func BuildAMI(ctx context.Context, o BuildOptions, a AMIOptions) (string, error) {
	if o.Arch != "amd64" {
		return "", fmt.Errorf("AMIs are built for amd64 only")
	}
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return "", fmt.Errorf("AWS configuration: %w", err)
	}
	c := ec2.NewFromConfig(cfg)
	say := func(f string, args ...any) { fmt.Fprintf(o.Out, "==> "+f+"\n", args...) }

	stage, err := StageBuild(ctx, o)
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)

	p, err := ssm.NewFromConfig(cfg).GetParameter(ctx, &ssm.GetParameterInput{Name: aws.String(ubuntuAMIParam)})
	if err != nil {
		return "", fmt.Errorf("finding the Ubuntu AMI: %w", err)
	}
	base := aws.ToString(p.Parameter.Value)
	imgs, err := c.DescribeImages(ctx, &ec2.DescribeImagesInput{ImageIds: []string{base}})
	if err != nil || len(imgs.Images) == 0 {
		return "", fmt.Errorf("describing %s: %v", base, err)
	}
	rootDev := imgs.Images[0].RootDeviceName

	b := &EC2{Subnet: a.Subnet, SecurityGroups: a.SecurityGroups, InstanceType: firstSet(a.InstanceType, EC2InstanceType),
		Session: "build-" + o.Image, client: c}
	b.address = b.Address
	pub, err := b.newKey(ctx)
	if err != nil {
		return "", err
	}
	defer b.removeKey()
	in := b.RunInput(base, cloudConfig(pub, "127.0.0.1", ""))
	// The builder stops when vm-export.sh powers it off, so its disk can be
	// saved; it's terminated afterwards.
	in.InstanceInitiatedShutdownBehavior = types.ShutdownBehaviorStop
	// The same disk size as the Lima VM.
	in.BlockDeviceMappings = []types.BlockDeviceMapping{{DeviceName: rootDev, Ebs: &types.EbsBlockDevice{
		VolumeSize: aws.Int32(20), VolumeType: types.VolumeTypeGp3, DeleteOnTermination: aws.Bool(true)}}}
	in.TagSpecifications[0].Tags[0].Value = aws.String("opsschool-build-" + o.Image)
	say("starting a builder from %s", base)
	out, err := c.RunInstances(ctx, in)
	if err != nil {
		return "", fmt.Errorf("starting the builder: %w", err)
	}
	b.instance = aws.ToString(out.Instances[0].InstanceId)
	defer func() {
		say("terminating the builder %s", b.instance)
		c.TerminateInstances(context.Background(), &ec2.TerminateInstancesInput{InstanceIds: []string{b.instance}})
	}()
	w, err := ec2.NewInstanceRunningWaiter(c).WaitForOutput(ctx, &ec2.DescribeInstancesInput{InstanceIds: []string{b.instance}}, 10*time.Minute)
	if err != nil {
		return "", fmt.Errorf("the builder %s did not start: %w", b.instance, err)
	}
	inst := w.Reservations[0].Instances[0]
	b.ip = firstSet(aws.ToString(inst.PublicIpAddress), aws.ToString(inst.PrivateIpAddress))

	say("waiting for %s (%s) to boot", b.instance, b.ip)
	if err := waitSSH(ctx, b, 10*time.Minute); err != nil {
		return "", err
	}
	if err := b.CopyIn(ctx, stage, "/tmp/opsschool-build"); err != nil {
		return "", err
	}
	if err := b.CopyIn(ctx, filepath.Join(o.Root, "images", "vm-export.sh"), "/tmp/vm-export.sh"); err != nil {
		return "", err
	}
	steps := []struct{ msg, script string }{
		{"provisioning (10-20 minutes)", "bash /tmp/opsschool-build/provision.sh /tmp/opsschool-build"},
		{"smoke test", "bash /tmp/opsschool-build/smoke.sh"},
		// The image's scripts expect the network card to be eth0.
		{"naming the network card eth0", `rm -rf /tmp/opsschool-build
echo 'GRUB_CMDLINE_LINUX_DEFAULT="$GRUB_CMDLINE_LINUX_DEFAULT net.ifnames=0"' >/etc/default/grub.d/90-opsschool-eth0.cfg
update-grub`},
		{"resetting the machine for its next boot", "bash /tmp/vm-export.sh " + vmUser},
	}
	for _, s := range steps {
		say("%s", s.msg)
		res, err := b.Run(ctx, s.script, nil)
		if err != nil {
			return "", fmt.Errorf("%s: %w", s.msg, err)
		}
		fmt.Fprint(o.Out, res.Stdout, res.Stderr)
		if res.ExitCode != 0 {
			return "", fmt.Errorf("%s: exit %d", s.msg, res.ExitCode)
		}
	}
	say("waiting for the builder to power off")
	if err := ec2.NewInstanceStoppedWaiter(c).Wait(ctx, &ec2.DescribeInstancesInput{InstanceIds: []string{b.instance}}, 10*time.Minute); err != nil {
		return "", fmt.Errorf("the builder did not stop: %w", err)
	}
	fp := o.Fingerprint
	tags := []types.Tag{
		{Key: aws.String("Name"), Value: aws.String("opsschool-" + o.Image)},
		{Key: aws.String(ec2ImageTag), Value: aws.String(o.Image)},
		{Key: aws.String(ec2FingerprintTag), Value: aws.String(fp)},
	}
	name := fmt.Sprintf("opsschool-%s-%s", o.Image, time.Now().UTC().Format("20060102-150405"))
	say("saving %s", name)
	img, err := c.CreateImage(ctx, &ec2.CreateImageInput{
		InstanceId: aws.String(b.instance), Name: aws.String(name),
		Description: aws.String("Ops School " + o.Image + " scenario machine, fingerprint " + fp),
		TagSpecifications: []types.TagSpecification{
			{ResourceType: types.ResourceTypeImage, Tags: tags},
			{ResourceType: types.ResourceTypeSnapshot, Tags: tags},
		},
	})
	if err != nil {
		return "", fmt.Errorf("saving the AMI: %w", err)
	}
	ami := aws.ToString(img.ImageId)
	if err := ec2.NewImageAvailableWaiter(c).Wait(ctx, &ec2.DescribeImagesInput{ImageIds: []string{ami}}, 45*time.Minute); err != nil {
		return "", fmt.Errorf("the AMI %s did not become available: %w", ami, err)
	}
	return ami, nil
}

// waitSSH waits until the machine takes SSH logins and cloud-init is done.
func waitSSH(ctx context.Context, s *EC2, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		res, err := s.Run(ctx, "cloud-init status --wait >/dev/null 2>&1 || true", nil)
		if err == nil && res.ExitCode == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			msg := ""
			if err != nil {
				msg = err.Error()
			} else {
				msg = strings.TrimSpace(res.Stderr)
			}
			return fmt.Errorf("could not log in to the builder within %s (%s); check that this machine can reach it on port 22", timeout, msg)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}
