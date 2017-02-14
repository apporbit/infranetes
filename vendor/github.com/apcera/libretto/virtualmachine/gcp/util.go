// Copyright 2016 Apcera Inc. All rights reserved.

package gcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2/jwt"

	googlecloud "google.golang.org/api/compute/v1"
)

var (
	// OAuth token url.
	tokenURL = "https://accounts.google.com/o/oauth2/token"
)

type googleService struct {
	vm      *VM
	service *googlecloud.Service
}

// accountFile represents the structure of the account file JSON file.
type accountFile struct {
	PrivateKey  string `json:"private_key"`
	ClientEmail string `json:"client_email"`
	ClientId    string `json:"client_id"`
}

func (vm *VM) getService() (*googleService, error) {
	var err error
	var client *http.Client

	if err = parseAccountFile(&vm.account, vm.AccountFile); err != nil {
		return nil, err
	}

	// Auth with AccountFile first if provided
	if vm.account.PrivateKey != "" {
		config := jwt.Config{
			Email:      vm.account.ClientEmail,
			PrivateKey: []byte(vm.account.PrivateKey),
			Scopes:     vm.Scopes,
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

	return &googleService{vm, svc}, nil
}

// get instance from current VM definition.
func (svc *googleService) getInstance() (*googlecloud.Instance, error) {
	return svc.service.Instances.Get(svc.vm.Project, svc.vm.Zone, svc.vm.Name).Do()
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

// waitForOperationReady waits for the regional operation to finish.
func (svc *googleService) waitForOperationReady(operation string) error {
	return waitForOperation(OperationTimeout, func() (*googlecloud.Operation, error) {
		return svc.service.ZoneOperations.Get(svc.vm.Project, svc.vm.Zone, operation).Do()
	})
}

func (svc *googleService) getImage() (*googlecloud.Image, error) {
	for _, img := range svc.vm.ImageProjects {
		image, err := svc.service.Images.Get(img, svc.vm.SourceImage).Do()
		if err == nil && image != nil && image.SelfLink != "" {
			return image, nil
		}
		image = nil
	}

	err := fmt.Errorf("could not find image %s in these projects: %s", svc.vm.SourceImage, svc.vm.ImageProjects)
	return nil, err
}

// createDisks creates non-booted disk.
func (svc *googleService) createDisks() (disks []*googlecloud.AttachedDisk, err error) {
	if len(svc.vm.Disks) == 0 {
		return nil, errors.New("no disks were found")
	}

	image, err := svc.getImage()
	if err != nil {
		return nil, err
	}

	for i, disk := range svc.vm.Disks {
		if i == 0 {
			// First one is booted device, it will created in VM provision stage
			disks = append(disks, &googlecloud.AttachedDisk{
				Type:       "PERSISTENT",
				Mode:       "READ_WRITE",
				Kind:       "compute#attachedDisk",
				Boot:       true,
				AutoDelete: disk.AutoDelete,
				InitializeParams: &googlecloud.AttachedDiskInitializeParams{
					SourceImage: image.SelfLink,
					DiskSizeGb:  int64(disk.DiskSizeGb),
					DiskType:    fmt.Sprintf("zones/%s/diskTypes/%s", svc.vm.Zone, disk.DiskType),
				},
			})
			continue
		}

		// Reuse the existing disk, create non-booted devices if it does not exist
		searchDisk, _ := svc.getDisk(disk.Name)
		if searchDisk == nil {
			d := &googlecloud.Disk{
				Name:   disk.Name,
				SizeGb: int64(disk.DiskSizeGb),
				Type:   fmt.Sprintf("zones/%s/diskTypes/%s", svc.vm.Zone, disk.DiskType),
			}

			op, err := svc.service.Disks.Insert(svc.vm.Project, svc.vm.Zone, d).Do()
			if err != nil {
				return disks, fmt.Errorf("error while creating disk %s: %v", disk.Name, err)
			}

			err = svc.waitForOperationReady(op.Name)
			if err != nil {
				return disks, fmt.Errorf("error while waiting for the disk %s ready, error: %v", disk.Name, err)
			}
		}

		disks = append(disks, &googlecloud.AttachedDisk{
			DeviceName: disk.Name,
			Type:       "PERSISTENT",
			Mode:       "READ_WRITE",
			Boot:       false,
			AutoDelete: disk.AutoDelete,
			Source:     fmt.Sprintf("projects/%s/zones/%s/disks/%s", svc.vm.Project, svc.vm.Zone, disk.Name),
		})
	}

	return disks, nil
}

// getDisk retrieves the Disk object.
func (svc *googleService) getDisk(name string) (*googlecloud.Disk, error) {
	return svc.service.Disks.Get(svc.vm.Project, svc.vm.Zone, name).Do()
}

// deleteDisk deletes the persistent disk.
func (svc *googleService) deleteDisk(name string) error {
	op, err := svc.service.Disks.Delete(svc.vm.Project, svc.vm.Zone, name).Do()
	if err != nil {
		return err
	}

	return svc.waitForOperationReady(op.Name)
}

// deleteDisks deletes all the persistent disk.
func (svc *googleService) deleteDisks() (errs []error) {
	for _, disk := range svc.vm.Disks {
		err := svc.deleteDisk(disk.Name)
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// getIPs returns the IP addresses of the GCE instance.
func (svc *googleService) getIPs() ([]net.IP, error) {
	instance, err := svc.service.Instances.Get(svc.vm.Project, svc.vm.Zone, svc.vm.Name).Do()
	if err != nil {
		return nil, err
	}

	ips := make([]net.IP, 2)
	nic := instance.NetworkInterfaces[0]

	publicIP := nic.AccessConfigs[0].NatIP
	if publicIP == "" {
		return nil, errors.New("error while retrieving public IP")
	}

	privateIP := nic.NetworkIP
	if privateIP == "" {
		return nil, errors.New("error while retrieving private IP")
	}

	ips[PublicIP] = net.ParseIP(publicIP)
	ips[PrivateIP] = net.ParseIP(privateIP)

	return ips, nil
}

// provision a new googlecloud VM instance.
func (svc *googleService) provision() error {
	zone, err := svc.service.Zones.Get(svc.vm.Project, svc.vm.Zone).Do()
	if err != nil {
		return err
	}

	machineType, err := svc.service.MachineTypes.Get(svc.vm.Project, zone.Name, svc.vm.MachineType).Do()
	if err != nil {
		return err
	}

	network, err := svc.service.Networks.Get(svc.vm.Project, svc.vm.Network).Do()
	if err != nil {
		return err
	}

	// validate network
	if !network.AutoCreateSubnetworks && len(network.Subnetworks) > 0 {
		// Network appears to be in "custom" mode, so a subnetwork is required
		// libretto doesn't handle the network creation
		if svc.vm.Subnetwork == "" {
			return fmt.Errorf("a subnetwork must be specified")
		}
	}

	subnetworkSelfLink := ""
	if svc.vm.Subnetwork != "" {
		subnetwork, err := svc.service.Subnetworks.Get(svc.vm.Project, svc.vm.region(), svc.vm.Subnetwork).Do()
		if err != nil {
			return err
		}
		subnetworkSelfLink = subnetwork.SelfLink
	}

	accessconfig := googlecloud.AccessConfig{
		Name: "External NAT for Libretto",
		Type: "ONE_TO_ONE_NAT",
	}

	md := svc.getSSHKey()

	disks, err := svc.createDisks()
	if err != nil {
		return err
	}

	instance := &googlecloud.Instance{
		Name:        svc.vm.Name,
		Description: svc.vm.Description,
		Disks:       disks,
		MachineType: machineType.SelfLink,
		Metadata: &googlecloud.Metadata{
			Items: []*googlecloud.MetadataItems{
				{
					Key:   "sshKeys",
					Value: &md,
				},
			},
		},
		NetworkInterfaces: []*googlecloud.NetworkInterface{
			{
				AccessConfigs: []*googlecloud.AccessConfig{
					&accessconfig,
				},
				Network:    network.SelfLink,
				Subnetwork: subnetworkSelfLink,
				NetworkIP:  svc.vm.PrivateIPAddress,
			},
		},
		Scheduling: &googlecloud.Scheduling{
			Preemptible: svc.vm.Preemptible,
		},
		ServiceAccounts: []*googlecloud.ServiceAccount{
			{
				Email:  "default",
				Scopes: svc.vm.Scopes,
			},
		},
		Tags: &googlecloud.Tags{
			Items: svc.vm.Tags,
		},
	}

	op, err := svc.service.Instances.Insert(svc.vm.Project, zone.Name, instance).Do()
	if err != nil {
		return err
	}

	if err = svc.waitForOperationReady(op.Name); err != nil {
		return err
	}

	_, err = svc.getInstance()
	return err
}

// start starts a stopped GCE instance.
func (svc *googleService) start() error {
	instance, err := svc.getInstance()
	if err != nil {
		if !strings.Contains(err.Error(), "no instance found") {
			return err
		}
	}

	if instance == nil {
		return errors.New("no instance found")
	}

	op, err := svc.service.Instances.Start(svc.vm.Project, svc.vm.Zone, svc.vm.Name).Do()
	if err != nil {
		return err
	}

	return svc.waitForOperationReady(op.Name)
}

// stop halts a GCE instance.
func (svc *googleService) stop() error {
	_, err := svc.getInstance()
	if err != nil {
		if !strings.Contains(err.Error(), "no instance found") {
			return err
		}
		return fmt.Errorf("no instance found, %v", err)
	}

	op, err := svc.service.Instances.Stop(svc.vm.Project, svc.vm.Zone, svc.vm.Name).Do()
	if err != nil {
		return err
	}

	return svc.waitForOperationReady(op.Name)
}

// deletes the GCE instance.
func (svc *googleService) delete() error {
	op, err := svc.service.Instances.Delete(svc.vm.Project, svc.vm.Zone, svc.vm.Name).Do()
	if err != nil {
		return err
	}

	return svc.waitForOperationReady(op.Name)
}

// extract the region from zone name.
func (vm *VM) region() string {
	return vm.Zone[:len(vm.Zone)-2]
}

func parseAccountJSON(result interface{}, jsonText string) error {
	dec := json.NewDecoder(strings.NewReader(jsonText))
	return dec.Decode(result)
}

func parseAccountFile(file *accountFile, account string) error {
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

func (svc *googleService) getSSHKey() string {
	return fmt.Sprintf("%s:%s\n", svc.vm.SSHCreds.SSHUser, svc.vm.SSHPublicKey)
}

func (svc *googleService) insertSSHKey() error {
	md := svc.getSSHKey()
	instance, err := svc.getInstance()
	if err != nil {
		return err
	}

	op, err := svc.service.Instances.SetMetadata(svc.vm.Project, svc.vm.Zone, svc.vm.Name, &googlecloud.Metadata{
		Fingerprint: instance.Metadata.Fingerprint,
		Items: []*googlecloud.MetadataItems{
			{
				Key:   "sshKeys",
				Value: &md,
			},
		},
	}).Do()
	if err != nil {
		return err
	}

	return svc.waitForOperationReady(op.Name)
}
