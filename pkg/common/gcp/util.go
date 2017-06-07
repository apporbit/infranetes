package gcp

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2/jwt"

	"github.com/apcera/libretto/virtualmachine/gcp"
	googlecloud "google.golang.org/api/compute/v1"
)

const (
	infranetesLabelKey   = "infranetes"
	infranetesLabelValue = "true"
)

var (
	// OAuth token url.
	tokenURL = "https://accounts.google.com/o/oauth2/token"
	// OperationTimeout represents Maximum time(Second) to wait for operation ready.
	OperationTimeout = 180
)

type GceConfig struct {
	Zone        string
	SourceImage string
	Project     string
	Scope       string
	AuthFile    string
	Network     string
	Subnet      string
}

type account struct {
	PrivateKey  string `json:"private_key"`
	ClientEmail string `json:"client_email"`
	ClientId    string `json:"client_id"`
}

func parseAccountJSON(result interface{}, jsonText string) error {
	dec := json.NewDecoder(strings.NewReader(jsonText))
	return dec.Decode(result)
}

func parseAccountFile(file *account, account string) error {
	if err := parseAccountJSON(file, account); err != nil {
		if _, err = os.Stat(account); os.IsNotExist(err) {
			return fmt.Errorf("error finding account file: %s", account)
		}

		bytes, err := ioutil.ReadFile(account)
		if err != nil {
			return fmt.Errorf("error reading account file from path '%s': %s", file, err)
		}

		err = parseAccountJSON(file, string(bytes))
		if err != nil {
			return fmt.Errorf("error parsing account file: %s", err)
		}
	}

	return nil
}

type svcWrapper struct {
	Project string
	Zone    string
	Service *googlecloud.Service
}

func GetService(accountFile string, project string, zone string, scopes []string) (*svcWrapper, error) {
	var err error
	var client *http.Client

	var account account

	if err = parseAccountFile(&account, accountFile); err != nil {
		return nil, err
	}

	// Auth with AccountFile first if provided
	if account.PrivateKey != "" {
		config := jwt.Config{
			Email:      account.ClientEmail,
			PrivateKey: []byte(account.PrivateKey),
			Scopes:     scopes,
			TokenURL:   tokenURL,
		}
		client = config.Client(oauth2.NoContext)
	} else {
		client = &http.Client{
			Timeout: time.Duration(30 * time.Second),
			Transport: &oauth2.Transport{
				Source: google.ComputeTokenSource(""),
			},
		}
	}

	svc, err := googlecloud.New(client)
	if err != nil {
		return nil, err
	}

	return &svcWrapper{
		Project: project,
		Zone:    zone,
		Service: svc,
	}, nil
}

func (s *svcWrapper) AddRoute(name string, ip string) error {
	a := &googlecloud.Route{
		Kind:            "compute#route",
		Name:            name,
		Network:         "projects/engineering-lab/global/networks/default",
		DestRange:       ip,
		NextHopInstance: "projects/engineering-lab/zones/us-central1-b/instances/" + name,
	}

	op, err := s.Service.Routes.Insert(s.Project, a).Do()
	if err != nil {
		return err
	}

	err = s.waitForGlobalOperationReady(op.Name)
	if err != nil {
		return fmt.Errorf("AddRoute failed: %v", err)
	}

	return nil
}

func (s *svcWrapper) DelRoute(name string) error {
	op, err := s.Service.Routes.Delete(s.Project, name).Do()
	if err != nil {
		return err
	}

	err = s.waitForGlobalOperationReady(op.Name)
	if err != nil {
		return fmt.Errorf("DelRoute failed: %v", err)
	}

	return nil
}

// waitForOperationReady waits for the regional operation to finish.
func (s *svcWrapper) waitForZoneOperationReady(operation string) error {
	return waitForOperation(OperationTimeout, func() (*googlecloud.Operation, error) {
		return s.Service.ZoneOperations.Get(s.Project, s.Zone, operation).Do()
	})
}

// waitForOperationReady waits for the global operation to finish.
func (s *svcWrapper) waitForGlobalOperationReady(operation string) error {
	return waitForOperation(OperationTimeout, func() (*googlecloud.Operation, error) {
		return s.Service.GlobalOperations.Get(s.Project, operation).Do()
	})
}

// waitForOperation pulls to wait for the operation to finish.
func waitForOperation(timeout int, funcOperation func() (*googlecloud.Operation, error)) error {
	var op *googlecloud.Operation
	var err error

	for i := 0; i < timeout; i++ {
		op, err = funcOperation()
		if err != nil {
			return err
		}

		if op.Status == "DONE" {
			if op.Error != nil {
				return fmt.Errorf("operation error: %v", *op.Error.Errors[0])
			}
			return nil
		}
		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("operation timeout, operations status: %v", op.Status)
}

func addRoute(vm *gcp.VM, podIp string) error {
	s, err := GetService(vm.AccountFile, vm.Project, vm.Zone, vm.Scopes)
	if err != nil {
		return fmt.Errorf("Couldn't make service to access google cloud")
	}

	return s.AddRoute(vm.Name, podIp)

}

func delRoute(vm *gcp.VM) error {
	s, err := GetService(vm.AccountFile, vm.Project, vm.Zone, vm.Scopes)
	if err != nil {
		return fmt.Errorf("Couldn't make service to access google cloud")
	}

	return s.DelRoute(vm.Name)
}

func (s *svcWrapper) TagNewInstance(name string) error {
	i, err := s.Service.Instances.Get(s.Project, s.Zone, name).Do()
	if err != nil {
		return fmt.Errorf("TagNewInstance: Couldn't get instance: %v: %v", name, err)
	}

	req := &googlecloud.InstancesSetLabelsRequest{
		LabelFingerprint: i.LabelFingerprint,
		Labels:           map[string]string{infranetesLabelKey: infranetesLabelValue},
	}

	op, err := s.Service.Instances.SetLabels(s.Project, s.Zone, name, req).Do()
	err = s.waitForGlobalOperationReady(op.Name)
	if err != nil {
		return fmt.Errorf("TagNewInstance failed: %v", err)
	}

	return nil
}

func (s *svcWrapper) ListInstances() ([]*googlecloud.Instance, error) {
	images := []*googlecloud.Instance{}

	nextPageToken := ""

	for {
		list, err := s.Service.Instances.List(s.Project, s.Zone).PageToken(nextPageToken).Do()
		if err != nil {
			return nil, fmt.Errorf("ListInstances failed: %v", err)
		}

		for _, i := range list.Items {
			if i.Labels[infranetesLabelKey] == infranetesLabelValue {
				images = append(images, i)
			}
		}

		nextPageToken = list.NextPageToken

		if nextPageToken == "" {
			break
		}
	}

	return images, nil
}

func (s *svcWrapper) CreateDisk(vol string, size int64) error {
	d := &googlecloud.Disk{
		Name:   vol,
		SizeGb: size,
	}
	op, err := s.Service.Disks.Insert(s.Project, s.Zone, d).Do()
	if err != nil {
		return err
	}

	err = s.waitForZoneOperationReady(op.Name)
	if err != nil {
		return fmt.Errorf("CreateDisk failed: %v", err)
	}

	return nil
}

func (s *svcWrapper) AttachDisk(vol string, instance string, device string) error {
	// https://www.googleapis.com/compute/v1/
	source := "projects/" + s.Project + "/zones/" + s.Zone + "/disks/" + vol
	req := &googlecloud.AttachedDisk{
		Source:     source,
		DeviceName: device,
	}
	op, err := s.Service.Instances.AttachDisk(s.Project, s.Zone, instance, req).Do()
	if err != nil {
		return err
	}
	err = s.waitForZoneOperationReady(op.Name)
	if err != nil {
		return fmt.Errorf("AttachDisk failed: %v", err)
	}

	return nil
}

func (s *svcWrapper) DetatchDisk(instance string, device string) error {
	op, err := s.Service.Instances.DetachDisk(s.Project, s.Zone, instance, device).Do()
	if err != nil {
		return err
	}
	err = s.waitForZoneOperationReady(op.Name)
	if err != nil {
		return fmt.Errorf("AttachDisk failed: %v", err)
	}

	return nil
}
