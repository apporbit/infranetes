package main

import (
	"flag"
	"fmt"
	"github.com/apcera/libretto/ssh"
	"github.com/apcera/libretto/virtualmachine/aws"
	"io/ioutil"
	"path/filepath"
	"strings"
)

var (
	key = flag.String("key", "", "keypair to use for ec2")
)

func main() {
	flag.Parse()

	rawKey, err := ioutil.ReadFile(*key)
	if err != nil {
		return
	}

	vm := &aws.VM{
		Name:         "libretto-aws",
		AMI:          "ami-6df1e514",
		InstanceType: "t2.micro",
		SSHCreds: ssh.Credentials{
			SSHUser:       "ubuntu",
			SSHPrivateKey: string(rawKey),
		},
		Volumes: []aws.EBSVolume{
			{
				DeviceName: "/dev/sda1",
			},
		},
		Region:        "us-west-2",
		KeyPair:       strings.TrimSuffix(filepath.Base(*key), filepath.Ext(*key)),
		SecurityGroups: []string{"sg-9272b4ea"},
		Subnet:        "subnet-0efb9a56",
	}

	err = vm.Provision()
	if err != nil {
		fmt.Printf("Failed to provision: %v\n", err)
		return
	}

	state, err := vm.GetState()
	if err != nil {
		fmt.Printf("Failed to get vm state: %v\n", err)
		return
	}
	fmt.Printf("vm state = %v\n", state)

	ips, err := vm.GetIPs()
	if err != nil {
		fmt.Printf("failed to get vm ips: %v\n", err)
		return
	}
	fmt.Printf("VM ips = %+v\n", ips)

	err = vm.Halt()
	if err != nil {
		fmt.Printf("failed to halt vm: %v\n", err)
		return
	}

	err = vm.Destroy()
	if err != nil {
		fmt.Printf("failed to destroy vm: %v\n", err)
	}
}
