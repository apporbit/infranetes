package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/apcera/libretto/ssh"
	vspherevm "github.com/apcera/libretto/virtualmachine/vsphere"
)

func main() {
	host := os.Getenv("GOVC_URL")
	username := os.Getenv("GOVC_USERNAME")
	password := os.Getenv("GOVC_PASSWORD")
	datastore := os.Getenv("GOVC_DATASTORE")
	network := os.Getenv("GOVC_NETWORK")
	datacenter := os.Getenv("GOVC_DATACENTER")

	insecure := true
	b, err := strconv.ParseBool(os.Getenv("GOVC_INSECURE"))
	if err == nil {
		insecure = b
	}

	vm := &vspherevm.VM{
		Host:       host,
		Username:   username,
		Password:   password,
		Datacenter: datacenter,
		Datastores: []string{datastore},
		Networks:   map[string]string{"nw1": network},
		Credentials: ssh.Credentials{
			SSHUser:     "ubuntu",
			SSHPassword: "ubuntu",
		},
		SkipExisting: true,
		Insecure:     insecure,
		Destination: vspherevm.Destination{
			//DestinationName: "pinkesh-lab",
			DestinationType: vspherevm.DestinationTypeHost,
			DestinationName: "209.205.216.10",
		},
		Name:     "shaya-test-vm",
		Template: "infranetes-base",
		OvfPath:  "/dev/null",
	}

	if err := vm.Provision(); err != nil {
		fmt.Printf("Failed to provision: %v\n", err)
		os.Exit(1)
	}

	ips, err := vm.GetIPs()
	if err != nil {
		fmt.Printf("Failed to get IPs: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("ips = %v", ips)
}
