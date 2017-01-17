package aws

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
)

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

func attachElasticIP(instanceID *string, elasticID *string) error {
	req := &ec2.AssociateAddressInput{
		AllocationId: elasticID,
		InstanceId:   instanceID,
	}

	_, err := client.AssociateAddress(req)
	if err != nil {
		return fmt.Errorf("AssociateAddress failed: %v", err)
	}

	return nil
}
