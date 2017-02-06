package main

import (
	"fmt"
	"net/url"
	"os"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
	"golang.org/x/net/context"
)

func main() {
	host := os.Getenv("GOVC_URL")
	username := os.Getenv("GOVC_USERNAME")
	password := os.Getenv("GOVC_PASSWORD")
	datacenter := os.Getenv("GOVC_DATACENTER")

	uri := getURI(host)
	u, err := url.Parse(uri)
	if err != nil || u.String() == "" {
		fmt.Printf("err = %v || u.String is empty\n", err)
		return
	}

	u.User = url.UserPassword(username, password)

	client, err := govmomi.NewClient(context.Background(), u, true)

	finder := find.NewFinder(client.Client, true)
	collector := property.DefaultCollector(client.Client)

	dc, err := finder.Datacenter(context.Background(), datacenter)

	dcMo := mo.Datacenter{}
	ps := []string{"name", "hostFolder", "vmFolder", "datastore"}
	err = collector.RetrieveOne(context.Background(), dc.Reference(), ps, &dcMo)
	if err != nil {
		return // err
	}
	if dcMo.Name != datacenter {
		fmt.Printf("Failed to find datacenter\n")
		return
	}

	vms, err := listVMs(collector, dcMo.VmFolder)
	for _, vm := range vms {
		//vm.Guest.GuestState
		ips := []string{}
		for _, n := range vm.Guest.Net {
			ips = append(ips, n.IpAddress...)
		}
		fmt.Printf("Vm %v = %v vs %v\n", vm.Name, ips, vm.Guest.IpAddress)
	}
}

func listVMs(collector *property.Collector, mor types.ManagedObjectReference) ([]*mo.VirtualMachine, error) {
	vms := []*mo.VirtualMachine{}

	switch mor.Type {
	case "Folder":
		// Fetch the childEntity property of the folder and check them
		folderMo := mo.Folder{}
		err := collector.RetrieveOne(context.Background(), mor, []string{"childEntity"}, &folderMo)
		if err != nil {
			fmt.Printf("listVMs: failed in RetrieveOne: %v", err)
			return nil, err
		}
		for _, child := range folderMo.ChildEntity {
			l_vms, e := listVMs(collector, child)
			if e != nil {
				// FIXME: handle error
				fmt.Printf("listVMs: failed in recursive listVMs call: %v", err)
				return nil, e
			}

			vms = append(vms, l_vms...)
		}

	case "VirtualMachine":
		// Base recursive case, compare for value
		vmMo := mo.VirtualMachine{}
		err := collector.RetrieveOne(context.Background(), mor, []string{"name", "guest.ipAddress", "guest.guestState", "guest.net", "runtime.question", "snapshot.currentSnapshot"}, &vmMo)
		if err != nil {
			fmt.Printf("listVMs: failed in RetrieveOne: %v", err)
			return nil, err
		}

		vms = append(vms, &vmMo)
	}

	return vms, nil
}

var getURI = func(host string) string {
	return fmt.Sprintf("https://%s/sdk", host)
}
