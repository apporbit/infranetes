package infranetes_flex

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"

	"github.com/apporbit/infranetes/pkg/infranetes_flex"
)

func init() {
	infranetes_flex.DevProviders.RegisterProvider("aws_ebs", NewAWSEBSProvider)
}

type awsEbsConfig struct {
	Region         string
	AvailZone      string
	AttachDevice   string
	SelfInstanceId string
}

type awsEbsProvider struct {
	ec2Client *ec2.EC2
	config    *awsEbsConfig
}

func NewAWSEBSProvider() (infranetes_flex.DevProvider, error) {
	var config awsEbsConfig

	file, err := ioutil.ReadFile("/root/aws_ebs.conf")
	if err != nil {
		return nil, fmt.Errorf("File error: %v\n", err)
	}

	json.Unmarshal(file, &config)

	creds := credentials.NewChainCredentials(
		[]credentials.Provider{
			&credentials.EnvProvider{},               // check environment
			&credentials.SharedCredentialsProvider{}, // check home dir
		},
	)

	ec2Client := ec2.New(session.New(&aws.Config{
		Credentials: creds,
		Region:      &config.Region,
		//CredentialsChainVerboseErrors: aws.Bool(true),
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}))

	return &awsEbsProvider{
		ec2Client: ec2Client,
		config:    &config,
	}, nil
}

func (p *awsEbsProvider) Provision(size uint64) (*string, error) {
	isize := int64(size)
	req := &ec2.CreateVolumeInput{
		Size:             &isize,
		AvailabilityZone: &p.config.AvailZone,
	}

	resp, err := p.ec2Client.CreateVolume(req)
	if err != nil {
		return nil, fmt.Errorf("provision failed: %v", err)
	}

	return resp.VolumeId, nil
}

func (p *awsEbsProvider) Attach(vol *string) (*string, error) {
	req := &ec2.AttachVolumeInput{
		VolumeId:   vol,
		Device:     &p.config.AttachDevice,
		InstanceId: &p.config.SelfInstanceId,
	}

	_, err := p.ec2Client.AttachVolume(req)
	if err != nil {
		return nil, err
	}

	if err := p.wait_attach(vol); err != nil {
		return nil, fmt.Errorf("do_attach: attach never being active: %v", err)
	}

	return &p.config.AttachDevice, nil
}

func (p *awsEbsProvider) wait_attach(vol *string) error {
	req := &ec2.DescribeVolumesInput{
		VolumeIds: []*string{vol},
	}

	for i := 1; i <= 5; i++ {
		resp, err := p.ec2Client.DescribeVolumes(req)
		if err != nil {
			return err
		}

		if len(resp.Volumes) != 1 {
			return fmt.Errorf("wait_attach: describe didn't return any volumes")
		}
		if len(resp.Volumes[0].Attachments) == 1 {
			if "attached" == *resp.Volumes[0].Attachments[0].State {
				return nil
			}
		}

		time.Sleep(time.Duration(i) * time.Second)
	}

	return fmt.Errorf("timed out waiting for volume attachment")
}

func (p *awsEbsProvider) Detach(vol *string) error {
	req := &ec2.DetachVolumeInput{
		VolumeId: vol,
	}

	_, err := p.ec2Client.DetachVolume(req)
	if err != nil {
		return err
	}

	if err := p.wait_detach(vol); err != nil {
		return fmt.Errorf("do_detach: detach never succeeded: %v", err)
	}

	return nil
}

func (p *awsEbsProvider) wait_detach(vol *string) error {
	req := &ec2.DescribeVolumesInput{
		VolumeIds: []*string{vol},
	}

	for i := 1; i <= 5; i++ {
		resp, err := p.ec2Client.DescribeVolumes(req)
		if err != nil {
			return err
		}

		if len(resp.Volumes) != 1 {
			return fmt.Errorf("wait_detach: describe didn't return any volumes")
		}

		if "available" == *resp.Volumes[0].State {
			return nil
		}

		time.Sleep(time.Duration(i) * time.Second)
	}

	return fmt.Errorf("timed out waiting for volume detachment")
}
