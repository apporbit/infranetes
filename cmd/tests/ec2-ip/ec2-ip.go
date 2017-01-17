package main

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

func main() {
	if len(os.Args) != 3 {
		fmt.Printf("Usage:\t ec2-ip <elastic ip allocation id> <intance id>\n")
		return
	}

	initEC2("us-west-2")

	req := &ec2.AssociateAddressInput{
		AllocationId: &os.Args[1],
		InstanceId:   &os.Args[2],
	}

	resp, err := client.AssociateAddress(req)
	if err != nil {
		fmt.Printf("AssociatedAddress failed: %v\n", err)
		return
	}

	fmt.Printf("AssociatedAddress worked: AssociationId = %v\n", resp.AssociationId)
}

func initEC2(region string) {
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

	client = ec2.New(session.New(&aws.Config{
		Credentials: creds,
		Region:      &region,
		//CredentialsChainVerboseErrors: aws.Bool(true),
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}))

}
