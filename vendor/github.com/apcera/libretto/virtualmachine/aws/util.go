// Copyright 2015 Apcera Inc. All rights reserved.

package aws

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/apcera/util/uuid"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
)

const (
	noCredsCode  = "NoCredentialProviders"
	noRegionCode = "MissingRegion"

	instanceCount       = 1
	defaultInstanceType = "t2.micro"
	defaultAMI          = "ami-5189a661" // ubuntu free tier
	defaultVolumeSize   = 8              // GB
	defaultDeviceName   = "/dev/sda1"
	defaultVolumeType   = "gp2"

	// RegionEnv is the env var for the AWS region.
	RegionEnv = "AWS_DEFAULT_REGION"
)

// ValidCredentials sends a dummy request to AWS to check if credentials are
// valid. An error is returned if credentials are missing or region is missing.
func ValidCredentials(region string) error {
	_, err := getService(region).DescribeInstances(nil)
	awsErr, isAWS := err.(awserr.Error)
	if !isAWS {
		return err
	}

	switch awsErr.Code() {
	case noCredsCode:
		return ErrNoCreds
	case noRegionCode:
		return ErrNoRegion
	}

	return nil
}

func getInstanceVolumeIDs(svc *ec2.EC2, instID string) ([]string, error) {
	resp, err := svc.DescribeVolumes(&ec2.DescribeVolumesInput{
		Filters: []*ec2.Filter{
			{Name: aws.String("attachment.instance-id"),
				Values: []*string{aws.String(instID)}},
		},
	})
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(resp.Volumes))
	for _, v := range resp.Volumes {
		if v == nil || v.VolumeId == nil {
			continue
		}

		ids = append(ids, *v.VolumeId)
	}

	return ids, nil
}

func getNonRootDeviceNames(svc *ec2.EC2, instID string) ([]string, error) {
	resp, err := svc.DescribeInstanceAttribute(&ec2.DescribeInstanceAttributeInput{
		Attribute:  aws.String("blockDeviceMapping"),
		InstanceId: aws.String(instID),
	})
	if err != nil {
		return nil, err
	}

	var rootDevice string
	if resp.RootDeviceName != nil && resp.RootDeviceName.Value != nil {
		rootDevice = *resp.RootDeviceName.Value
	}

	names := make([]string, 0, len(resp.BlockDeviceMappings))
	for _, m := range resp.BlockDeviceMappings {
		if m == nil || m.DeviceName == nil {
			continue
		}

		if *m.DeviceName == rootDevice {
			continue
		}

		names = append(names, *m.DeviceName)
	}

	return names, nil
}

func setNonRootDeleteOnDestroy(svc *ec2.EC2, instID string, delOnTerm bool) error {
	devNames, err := getNonRootDeviceNames(svc, instID)
	if err != nil {
		return fmt.Errorf("DescribeInstanceAttribute: %s", err)
	}

	devices := make([]*ec2.InstanceBlockDeviceMappingSpecification, 0, len(devNames))
	for _, name := range devNames {
		devices = append(devices, &ec2.InstanceBlockDeviceMappingSpecification{
			DeviceName: aws.String(name),
			Ebs: &ec2.EbsInstanceBlockDeviceSpecification{
				DeleteOnTermination: aws.Bool(delOnTerm),
			},
		})
	}

	_, err = svc.ModifyInstanceAttribute(&ec2.ModifyInstanceAttributeInput{
		InstanceId:          aws.String(instID),
		BlockDeviceMappings: devices,
	})
	if err != nil {
		return fmt.Errorf("ModifyInstanceAttribute: %s", err)
	}

	return nil
}

func getService(region string) *ec2.EC2 {
	creds := credentials.NewChainCredentials(
		[]credentials.Provider{
			&credentials.EnvProvider{},               // check environment
			&credentials.SharedCredentialsProvider{}, // check home dir
		},
	)

	if region == "" { // user didn't set region
		region = os.Getenv("AWS_DEFAULT_REGION") // aws cli checks this
		if region == "" {
			region = os.Getenv("AWS_REGION") // aws sdk checks this
		}
	}

	return ec2.New(session.New(&aws.Config{
		Credentials: creds,
		Region:      &region,
		CredentialsChainVerboseErrors: aws.Bool(true),
		HTTPClient:                    &http.Client{Timeout: 30 * time.Second},
	}))
}

func instanceInfo(vm *VM) *ec2.RunInstancesInput {
	if vm.Name == "" {
		vm.Name = fmt.Sprintf("libretto-vm-%s", uuid.Variant4())
	}
	if vm.AMI == "" {
		vm.AMI = defaultAMI
	}
	if vm.InstanceType == "" {
		vm.InstanceType = defaultInstanceType
	}

	var iamInstance *ec2.IamInstanceProfileSpecification
	if vm.IamInstanceProfileName != "" {
		iamInstance = &ec2.IamInstanceProfileSpecification{
			Name: aws.String(vm.IamInstanceProfileName),
		}
	}

	var sid *string
	if vm.Subnet != "" {
		sid = aws.String(vm.Subnet)
	}

	var sgid []*string
	if vm.SecurityGroup != "" {
		sgid = make([]*string, 1)
		sgid[0] = aws.String(vm.SecurityGroup)
	}

	devices := make([]*ec2.BlockDeviceMapping, len(vm.Volumes))
	for _, volume := range vm.Volumes {
		if volume.VolumeSize == 0 {
			volume.VolumeSize = defaultVolumeSize
		}
		if volume.VolumeType == "" {
			volume.VolumeType = defaultVolumeType
		}

		devices = append(devices, &ec2.BlockDeviceMapping{
			DeviceName: aws.String(volume.DeviceName),
			Ebs: &ec2.EbsBlockDevice{
				VolumeSize:          aws.Int64(int64(volume.VolumeSize)),
				VolumeType:          aws.String(volume.VolumeType),
				DeleteOnTermination: aws.Bool(!vm.KeepRootVolumeOnDestroy),
			},
		})
	}

	return &ec2.RunInstancesInput{
		ImageId:             aws.String(vm.AMI),
		InstanceType:        aws.String(vm.InstanceType),
		KeyName:             aws.String(vm.KeyPair),
		MaxCount:            aws.Int64(instanceCount),
		MinCount:            aws.Int64(instanceCount),
		BlockDeviceMappings: devices,
		Monitoring: &ec2.RunInstancesMonitoringEnabled{
			Enabled: aws.Bool(true),
		},
		SubnetId:           sid,
		SecurityGroupIds:   sgid,
		IamInstanceProfile: iamInstance,
	}
}

func hasInstanceID(instance *ec2.Instance) bool {
	if instance == nil || instance.InstanceId == nil {
		return false
	}

	return true
}
