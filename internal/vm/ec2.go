package vm

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// EC2 runs the scenario machine as an EC2 instance, for hosted mode where
// the cluster can't run VMs itself: a portal on EKS without metal nodes,
// for example. Each session gets an instance from an AMI built by
// BuildAMI, the same machine Lima and KubeVirt boot, and the runner
// reaches it over SSH on the VPC network. See docs/hosted.md.
type EC2 struct {
	// Image is an AMI ID, or "" to use the newest AMI that BuildAMI made
	// for the scenario's image in this account and region.
	Image        string
	InstanceType string
	// Subnet and SecurityGroups place the instance. The groups must let the
	// runner reach it (SSH and the exporters) and let it reach the runner's
	// Loki port.
	Subnet         string
	SecurityGroups []string
	// LogHost is the address Alloy on the machine pushes logs to.
	LogHost string
	// Session tags the instance.
	Session string
	// Lease is how long the instance may live. It powers itself off after
	// that, which terminates it, in case the runner never deletes it.
	Lease time.Duration

	client   *ec2.Client
	instance string
	ip       string
	sshVM
}

// EC2 settings. An instance is sized like the Lima VM: some scenarios
// depend on the memory size.
const (
	EC2InstanceType = "c7i.large" // 2 vCPUs, 4 GiB
	// ec2ImageTag and ec2FingerprintTag mark AMIs that BuildAMI made.
	ec2ImageTag       = "opsschool:image"
	ec2FingerprintTag = "opsschool:fingerprint"
	ec2SessionTag     = "opsschool:session"
)

// EC2FromEnv configures the driver from the environment the portal gives
// a session runner pod. AWS credentials and region come from the usual
// places: on EKS, from the session service account's IAM role.
func EC2FromEnv() (*EC2, error) {
	e := &EC2{
		Image:        os.Getenv("OPSSCHOOL_MACHINE_IMAGE"),
		InstanceType: firstSet(os.Getenv("OPSSCHOOL_EC2_INSTANCE_TYPE"), EC2InstanceType),
		Subnet:       os.Getenv("OPSSCHOOL_EC2_SUBNET"),
		LogHost:      os.Getenv("OPSSCHOOL_POD_IP"),
		Session:      os.Getenv("OPSSCHOOL_SESSION"),
		Lease:        4 * time.Hour,
	}
	if s := os.Getenv("OPSSCHOOL_EC2_SECURITY_GROUPS"); s != "" {
		e.SecurityGroups = strings.Split(s, ",")
	}
	if s := os.Getenv("OPSSCHOOL_MAX_AGE"); s != "" {
		d, err := time.ParseDuration(s)
		if err != nil {
			return nil, fmt.Errorf("OPSSCHOOL_MAX_AGE: %w", err)
		}
		// A little longer than the portal allows, so the portal ends the
		// session first.
		e.Lease = d + 30*time.Minute
	}
	if e.Subnet == "" || e.LogHost == "" {
		return nil, fmt.Errorf("the ec2 driver runs inside a session pod; OPSSCHOOL_EC2_SUBNET and OPSSCHOOL_POD_IP must be set")
	}
	return e, nil
}

func firstSet(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

func (e *EC2) Name() string             { return "ec2" }
func (e *EC2) TelemetryNetwork() string { return "" }

func (e *EC2) ec2(ctx context.Context) (*ec2.Client, error) {
	if e.client != nil {
		return e.client, nil
	}
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("AWS configuration: %w", err)
	}
	e.client = ec2.NewFromConfig(cfg)
	return e.client, nil
}

// Base reports whether an AMI for the image exists, and its fingerprint.
func (e *EC2) Base(ctx context.Context, image string) (bool, string, error) {
	c, err := e.ec2(ctx)
	if err != nil {
		return false, "", err
	}
	ami, err := newestAMI(ctx, c, image)
	if err != nil || ami == nil {
		return false, "", err
	}
	return true, tag(ami.Tags, ec2FingerprintTag), nil
}

// newestAMI finds the latest AMI that BuildAMI made for an image, or nil.
func newestAMI(ctx context.Context, c *ec2.Client, image string) (*types.Image, error) {
	out, err := c.DescribeImages(ctx, &ec2.DescribeImagesInput{
		Owners:  []string{"self"},
		Filters: []types.Filter{{Name: aws.String("tag:" + ec2ImageTag), Values: []string{image}}},
	})
	if err != nil {
		return nil, fmt.Errorf("finding the %s AMI: %w", image, err)
	}
	imgs := out.Images
	if len(imgs) == 0 {
		return nil, nil
	}
	sort.Slice(imgs, func(i, j int) bool { return aws.ToString(imgs[i].CreationDate) > aws.ToString(imgs[j].CreationDate) })
	return &imgs[0], nil
}

func tag(ts []types.Tag, key string) string {
	for _, t := range ts {
		if aws.ToString(t.Key) == key {
			return aws.ToString(t.Value)
		}
	}
	return ""
}

// leaseConfig makes the instance power itself off, which terminates it,
// once its lease is up. It's set on every boot, so it survives reboots.
func leaseConfig(until time.Time) string {
	return fmt.Sprintf("  - systemd-run --unit instance-lease --on-calendar '%s' /usr/bin/systemctl poweroff || true\n",
		until.UTC().Format("2006-01-02 15:04:05 UTC"))
}

// RunInput is the instance request, without the AMI and user data.
func (e *EC2) RunInput(ami, userData string) *ec2.RunInstancesInput {
	tags := []types.Tag{
		{Key: aws.String("Name"), Value: aws.String("opsschool-machine-" + e.Session)},
		{Key: aws.String(ec2SessionTag), Value: aws.String(e.Session)},
	}
	return &ec2.RunInstancesInput{
		ImageId:          aws.String(ami),
		InstanceType:     types.InstanceType(e.InstanceType),
		MinCount:         aws.Int32(1),
		MaxCount:         aws.Int32(1),
		SubnetId:         aws.String(e.Subnet),
		SecurityGroupIds: e.SecurityGroups,
		UserData:         aws.String(base64.StdEncoding.EncodeToString([]byte(userData))),
		// Powering off from inside, at the end of the lease, ends it.
		InstanceInitiatedShutdownBehavior: types.ShutdownBehaviorTerminate,
		// Learners are root: keep the instance's own credentials, if it
		// ever gets any, behind IMDSv2.
		MetadataOptions: &types.InstanceMetadataOptionsRequest{HttpTokens: types.HttpTokensStateRequired},
		TagSpecifications: []types.TagSpecification{
			{ResourceType: types.ResourceTypeInstance, Tags: tags},
			{ResourceType: types.ResourceTypeVolume, Tags: tags},
		},
	}
}

func (e *EC2) Create(ctx context.Context, image string) error {
	c, err := e.ec2(ctx)
	if err != nil {
		return err
	}
	ami := e.Image
	if ami == "" || strings.Contains(ami, "{image}") {
		img, err := newestAMI(ctx, c, image)
		if err != nil {
			return err
		}
		if img == nil {
			return fmt.Errorf("no AMI for %s in this account and region; build one with opsschool image build %s --driver ec2", image, image)
		}
		ami = aws.ToString(img.ImageId)
	}
	e.address = e.Address
	pub, err := e.newKey(ctx)
	if err != nil {
		return err
	}
	ud := cloudConfig(pub, e.LogHost, leaseConfig(time.Now().Add(e.Lease)))
	out, err := c.RunInstances(ctx, e.RunInput(ami, ud))
	if err != nil {
		return fmt.Errorf("starting the machine: %w", err)
	}
	e.instance = aws.ToString(out.Instances[0].InstanceId)
	if err := e.waitRunning(ctx, c, 10*time.Minute); err != nil {
		return err
	}
	return waitBooted(ctx, e, 5*time.Minute)
}

func (e *EC2) waitRunning(ctx context.Context, c *ec2.Client, timeout time.Duration) error {
	w := ec2.NewInstanceRunningWaiter(c)
	out, err := w.WaitForOutput(ctx, &ec2.DescribeInstancesInput{InstanceIds: []string{e.instance}}, timeout)
	if err != nil {
		return fmt.Errorf("the machine (%s) did not start: %w", e.instance, err)
	}
	inst := out.Reservations[0].Instances[0]
	e.ip = aws.ToString(inst.PrivateIpAddress)
	if e.ip == "" {
		return fmt.Errorf("the machine (%s) has no address", e.instance)
	}
	return nil
}

// Address returns the instance's private address.
func (e *EC2) Address(ctx context.Context) (string, error) {
	if e.ip == "" {
		return "", errors.New("the machine has no address")
	}
	return e.ip, nil
}

func (e *EC2) Exists(ctx context.Context) (bool, error) {
	if e.instance == "" {
		return false, nil
	}
	c, err := e.ec2(ctx)
	if err != nil {
		return false, err
	}
	out, err := c.DescribeInstances(ctx, &ec2.DescribeInstancesInput{InstanceIds: []string{e.instance}})
	if err != nil {
		return false, err
	}
	for _, r := range out.Reservations {
		for _, i := range r.Instances {
			if i.State != nil && i.State.Name != types.InstanceStateNameTerminated && i.State.Name != types.InstanceStateNameShuttingDown {
				return true, nil
			}
		}
	}
	return false, nil
}

func (e *EC2) Delete(ctx context.Context) error {
	e.removeKey()
	if e.instance == "" {
		return nil
	}
	c, err := e.ec2(ctx)
	if err != nil {
		return err
	}
	_, err = c.TerminateInstances(ctx, &ec2.TerminateInstancesInput{InstanceIds: []string{e.instance}})
	return err
}
