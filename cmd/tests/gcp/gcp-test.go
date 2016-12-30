package main

import (
	"fmt"
	gcpvm "github.com/apcera/libretto/virtualmachine/gcp"
)

func main() {
	vm := gcpvm.VM{
		Name:        "testy-mctester",
		Zone:        "us-west1-a",
		MachineType: "g1-small",
		SourceImage: "infranetes-base",
		Disks: []gcpvm.Disk{
			{
				DiskType:   "pd-standard",
				DiskSizeGb: 10,
				AutoDelete: true,
			},
		},
		Preemptible:   false,
		Network:       "default",
		Subnetwork:    "default",
		UseInternalIP: false,
		ImageProjects: []string{"engineering-lab"},
		Project:       "engineering-lab",
		Scopes:        []string{"https://www.googleapis.com/auth/cloud-platform"},
		AccountFile:   "/home/spotter/gcp.json",
	}

	err := vm.Provision()
	if err != nil {
		fmt.Printf("Provision failed: %v\n", err)
		return
	}

	state, err := vm.GetState()
	if err != nil {
		fmt.Printf("GetState failed: %v\n", err)
		return
	}

	fmt.Printf("State = %v", state)

	ips, err := vm.GetIPs()
	if err != nil {
		fmt.Printf("GetIPs failed: %v\n", err)
		return
	}

	fmt.Printf("IPs = %+v\n", ips)
	fmt.Printf("Name = %v\n", vm.GetName())
}
