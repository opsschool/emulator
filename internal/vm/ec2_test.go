package vm

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

func TestEC2RunInput(t *testing.T) {
	e := &EC2{InstanceType: "c7i.large", Subnet: "subnet-1", SecurityGroups: []string{"sg-1"}, Session: "s1"}
	until := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	in := e.RunInput("ami-1", cloudConfig("ssh-ed25519 AAAA runner", "10.0.0.9", leaseConfig(until)))
	if aws.ToString(in.ImageId) != "ami-1" || aws.ToString(in.SubnetId) != "subnet-1" || in.InstanceType != "c7i.large" {
		t.Errorf("request %+v", in)
	}
	if in.InstanceInitiatedShutdownBehavior != types.ShutdownBehaviorTerminate {
		t.Error("an instance that powers off must terminate")
	}
	if tag(in.TagSpecifications[0].Tags, ec2SessionTag) != "s1" {
		t.Error("instance not tagged with its session")
	}
	b, _ := base64.StdEncoding.DecodeString(aws.ToString(in.UserData))
	ud := string(b)
	for _, want := range []string{
		"#cloud-config\n", "- ssh-ed25519 AAAA runner\n", "echo '10.0.0.9 host.lima.internal'",
		"  - systemd-run --unit instance-lease --on-calendar '2026-10-05 12:00:00 UTC' /usr/bin/systemctl poweroff",
	} {
		if !strings.Contains(ud, want) {
			t.Errorf("user data lacks %q:\n%s", want, ud)
		}
	}
	// The lease is a bootcmd entry, so it's set again after a reboot.
	if strings.Index(ud, "instance-lease") < strings.Index(ud, "bootcmd:") {
		t.Errorf("lease outside bootcmd:\n%s", ud)
	}
}

func TestNewestAMI(t *testing.T) {
	var form string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		form = r.Form.Encode()
		w.Header().Set("Content-Type", "text/xml")
		w.Write([]byte(`<DescribeImagesResponse xmlns="http://ec2.amazonaws.com/doc/2016-11-15/"><imagesSet>
<item><imageId>ami-old</imageId><creationDate>2026-09-01T00:00:00.000Z</creationDate>
  <tagSet><item><key>opsschool:fingerprint</key><value>aaa</value></item></tagSet></item>
<item><imageId>ami-new</imageId><creationDate>2026-10-01T00:00:00.000Z</creationDate>
  <tagSet><item><key>opsschool:fingerprint</key><value>bbb</value></item></tagSet></item>
</imagesSet></DescribeImagesResponse>`))
	}))
	defer srv.Close()
	c := ec2.New(ec2.Options{
		Region: "us-east-1", BaseEndpoint: aws.String(srv.URL),
		Credentials: credentials.NewStaticCredentialsProvider("k", "s", ""),
	})
	e := &EC2{client: c}
	built, fp, err := e.Base(context.Background(), "single-node")
	if err != nil || !built || fp != "bbb" {
		t.Errorf("Base: %v %q %v", built, fp, err)
	}
	for _, want := range []string{"Owner.1=self", "Filter.1.Name=tag%3Aopsschool%3Aimage", "Filter.1.Value.1=single-node"} {
		if !strings.Contains(form, want) {
			t.Errorf("request lacks %s: %s", want, form)
		}
	}
}
