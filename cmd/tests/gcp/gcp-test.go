package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	gcpvm "github.com/apcera/libretto/virtualmachine/gcp"
)

type gceConfig struct {
	Zone        string
	SourceImage string
	Project     string
	Scope       string
	AuthFile    string
	Network     string
	Subnet      string
}

func main() {
	var conf gceConfig

	file, err := ioutil.ReadFile("gce.json")
	if err != nil {
		fmt.Printf("File error: %v\n", err)
		os.Exit(1)
	}

	json.Unmarshal(file, &conf)

	if conf.SourceImage == "" || conf.Zone == "" || conf.Project == "" || conf.Scope == "" || conf.AuthFile == "" || conf.Network == "" || conf.Subnet == "" {
		msg := fmt.Sprintf("Failed to read in complete config file: conf = %+v", conf)
		fmt.Println(msg)
		os.Exit(1)
	}

	vm := gcpvm.VM{
		Name:        "testy-mctester",
		Zone:        "us-west1-a",
		MachineType: "g1-small",
		SourceImage: conf.SourceImage,
		Disks: []gcpvm.Disk{
			{
				DiskType:   "pd-standard",
				DiskSizeGb: 10,
				AutoDelete: true,
			},
		},
		Preemptible:   false,
		Network:       conf.Network,
		Subnetwork:    conf.Subnet,
		UseInternalIP: false,
		ImageProjects: []string{conf.Project},
		Project:       conf.Project,
		Scopes:        []string{conf.Scope},
		AccountFile:   conf.AuthFile,
	}

	err = vm.Provision()
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
