package aws

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/golang/glog"

	awsvm "github.com/apcera/libretto/virtualmachine/aws"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ec2"
)

type awsAnnotations struct {
	ami           string
	role          string
	instanceType  string
	securityGroup string
	region        string
	subnet        string
	elasticIP     string
}

func parseAWSAnnotations(a map[string]string) *awsAnnotations {
	ret := &awsAnnotations{}

	if tmp, ok := a["infranetes.aws.image"]; ok {
		ret.ami = tmp
	}

	if tmp, ok := a["infranetes.aws.iaminstancename"]; ok {
		ret.role = tmp
	}

	if tmp, ok := a["infranetes.aws.instancetype"]; ok {
		ret.instanceType = tmp
	}

	if tmp, ok := a["infranetes.aws.securtiygroup"]; ok {
		ret.securityGroup = tmp
	}

	if tmp, ok := a["infranetes.aws.region"]; ok {
		ret.region = tmp
	}

	if tmp, ok := a["infranetes.aws.subnet"]; ok {
		ret.subnet = tmp
	}

	if tmp, ok := a["infranetes.aws.elasticip"]; ok {
		ret.elasticIP = tmp
	}

	return ret
}

func overrideVMDefault(vm *awsvm.VM, anno *awsAnnotations) {
	if anno.ami != "" {
		glog.Infof("ParseAWSAnnotations: overriding ami image with %v", anno.ami)
		vm.AMI = anno.ami
	}

	if anno.role != "" {
		glog.Infof("ParseAWSAnnotations: booting instance iam role %v", anno.role)
		vm.IamInstanceProfileName = anno.role
	}

	if anno.instanceType != "" {
		glog.Infof("ParseAWSAnnotations: booting instance type %v", anno.instanceType)
		vm.InstanceType = anno.instanceType
	}

	if anno.securityGroup != "" {
		glog.Infof("ParseAWSAnnotations: booting instance security group %v", anno.securityGroup)
		vm.SecurityGroup = anno.securityGroup
	}

	if anno.region != "" {
		glog.Infof("ParseAWSAnnotations: booting instance region %v", anno.region)
		vm.Region = anno.region
	}

	if anno.subnet != "" {
		glog.Infof("RunPodSandbox: booting instance subnet %v", anno.subnet)
		vm.Subnet = anno.subnet
	}
}

func findBase(subnetId *string) (*string, error) {
	req := &ec2.DescribeSubnetsInput{SubnetIds: []*string{subnetId}}
	resp, err := client.DescribeSubnets(req)
	if err != nil {
		awsErr, _ := err.(awserr.Error)
		msg := fmt.Sprintf("DescribeSubnets failed: msg = %v, code = %v", awsErr.Message(), awsErr.Code())
		glog.Error(msg)
		return nil, errors.New(msg)
	}
	if len(resp.Subnets) != 1 {
		msg := fmt.Sprintf("DescribeSubnets: Didn't return correct amount of subnets: subnets = %+v", resp.Subnets)
		glog.Error(msg)
		return nil, errors.New(msg)
	}

	subnet := resp.Subnets[0]

	if *subnet.MapPublicIpOnLaunch != true {
		msg := fmt.Sprintf("Subnet %v isn't configured correctly, doesn't provide public ip on launch", *subnet.SubnetId)
		glog.Error(msg)
		return nil, errors.New(msg)
	}

	base, err := baseFromCidr(subnet.CidrBlock)
	if err != nil {
		msg := fmt.Sprintf("Couldn't parse subnet's CIDR %v: %v", *subnet.CidrBlock, err)
		glog.Error(msg)
		return nil, errors.New(msg)
	}

	return base, err
}

func baseFromCidr(cidr *string) (*string, error) {
	ip, _, err := net.ParseCIDR(*cidr)
	if err != nil {
		return nil, err
	}

	splits := strings.Split(ip.String(), ".")
	if len(splits) != 4 {
		return nil, fmt.Errorf("%v can't be split correctly", ip.String())
	}

	base := strings.Join(splits[0:3], ".")
	glog.Infof("baseFromCidr: calculated base = %v", base)

	return &base, nil
}

func findMaster() (*string, bool) {
	filters := []*ec2.Filter{
		{
			Name:   aws.String("instance-state-name"),
			Values: []*string{aws.String("running")},
		},
	}

	request := ec2.DescribeInstancesInput{Filters: filters}
	result, err := client.DescribeInstances(&request)
	if err != nil {
		return nil, false
	}

	for _, resv := range result.Reservations {
		for _, instance := range resv.Instances {
			for _, tag := range instance.Tags {
				if "Name" == *tag.Key && "k8s-spotter-master" == *tag.Value {
					glog.Infof("Found master ip: %v", *instance.PrivateIpAddress)
					return instance.PrivateIpAddress, true
				}
			}
		}
	}

	return nil, false
}
