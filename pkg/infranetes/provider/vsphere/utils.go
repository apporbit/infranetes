package vsphere

import (
	"fmt"
	"net/url"

	"golang.org/x/net/context"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

func getClient(host, username, password string, insecure bool) (*govmomi.Client, context.CancelFunc, error) {
	uri := getURI(host)
	u, err := url.Parse(uri)
	if err != nil {
		return nil, nil, fmt.Errorf("lisgetClienttVMs: failed to prase host url: %v", err)
	}
	if u.String() == "" {
		return nil, nil, fmt.Errorf("getClient: got an empty uri")
	}

	u.User = url.UserPassword(username, password)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, err := govmomi.NewClient(ctx, u, insecure)
	if err != nil {
		return nil, nil, fmt.Errorf("getClient: NewClient failed: %v", err)
	}

	return client, cancel, nil
}

func verifyCreds(host, username, pasword string, insecure bool) error {
	_, cancel, err := getClient(host, username, pasword, insecure)
	if err == nil {
		defer cancel()
	}

	return err
}

func listVMs(host, username, password, datacenter string, insecure bool) ([]*mo.VirtualMachine, error) {
	client, cancel, err := getClient(host, username, password, insecure)
	if err != nil {
		return nil, fmt.Errorf("listVMs: %v", err)
	}
	defer cancel()

	finder := find.NewFinder(client.Client, true)
	collector := property.DefaultCollector(client.Client)

	dc, err := finder.Datacenter(context.Background(), datacenter)

	dcMo := mo.Datacenter{}
	ps := []string{"name", "hostFolder", "vmFolder", "datastore"}
	err = collector.RetrieveOne(context.Background(), dc.Reference(), ps, &dcMo)
	if err != nil {
		return nil, fmt.Errorf("listVMs: RetrieveOne failed: %v", err)
	}
	if dcMo.Name != datacenter {
		return nil, fmt.Errorf("listVMs: Failed to find datacenter\n")
	}

	return _listVMs(collector, dcMo.VmFolder)
}

func _listVMs(collector *property.Collector, mor types.ManagedObjectReference) ([]*mo.VirtualMachine, error) {
	vms := []*mo.VirtualMachine{}

	switch mor.Type {
	case "Folder":
		// Fetch the childEntity property of the folder and check them
		folderMo := mo.Folder{}
		err := collector.RetrieveOne(context.Background(), mor, []string{"childEntity"}, &folderMo)
		if err != nil {

			return nil, fmt.Errorf("listVMs: failed in RetrieveOne: %v", err)
		}
		for _, child := range folderMo.ChildEntity {
			l_vms, e := _listVMs(collector, child)
			if e != nil {
				// FIXME: handle errors
				return nil, fmt.Errorf("listVMs: failed in recursive listVMs call: %v", err)
			}

			vms = append(vms, l_vms...)
		}

	case "VirtualMachine":
		// Base recursive case, compare for value
		vmMo := mo.VirtualMachine{}
		err := collector.RetrieveOne(context.Background(), mor, []string{"name", "guest.ipAddress", "guest.guestState", "guest.net", "runtime.question", "snapshot.currentSnapshot"}, &vmMo)
		if err != nil {
			return nil, fmt.Errorf("listVMs: failed in RetrieveOne: %v", err)
		}

		vms = append(vms, &vmMo)
	}

	return vms, nil
}

var getURI = func(host string) string {
	return fmt.Sprintf("https://%s/sdk", host)
}
