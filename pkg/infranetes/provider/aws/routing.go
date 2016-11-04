package aws

import (
	"net/http"
	"os"
	"time"

	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
)

var (
	client *ec2.EC2
)

func InitEC2(region string) {
	client = getService(region)
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

func addRoute(routeTable string, instance string, ip string) error {
	cidr := fmt.Sprintf("%v/32", ip)

	req := &ec2.CreateRouteInput{
		DestinationCidrBlock: aws.String(cidr),
		InstanceId:           aws.String(instance),
		RouteTableId:         aws.String(routeTable),
	}

	_, err := client.CreateRoute(req)

	return err
}

func delRoute(routeTable string, ip string) error {
	cidr := fmt.Sprintf("%v/32", ip)

	req := &ec2.DeleteRouteInput{
		DestinationCidrBlock: aws.String(cidr),
		RouteTableId:         aws.String(routeTable),
	}

	_, err := client.DeleteRoute(req)

	return err
}

func destSourceReset(instance string) error {
	params := &ec2.ModifyInstanceAttributeInput{
		InstanceId: aws.String(instance),
		SourceDestCheck: &ec2.AttributeBooleanValue{
			Value: aws.Bool(false),
		},
	}

	_, err := client.ModifyInstanceAttribute(params)

	return err
}

func listInstances() ([]*ec2.Instance, error) {
	filters := []*ec2.Filter{
		{
			Name:   aws.String("instance-state-name"),
			Values: []*string{aws.String("running"), aws.String("pending")},
		},
	}

	request := ec2.DescribeInstancesInput{Filters: filters}
	result, err := client.DescribeInstances(&request)
	if err != nil {
		return nil, err
	}

	instances := []*ec2.Instance{}

	for _, resv := range result.Reservations {
		for _, instance := range resv.Instances {
			for _, tag := range instance.Tags {
				if "infranetes" == *tag.Key {
					instances = append(instances, instance)
				}
			}
		}
	}

	return instances, nil
}
