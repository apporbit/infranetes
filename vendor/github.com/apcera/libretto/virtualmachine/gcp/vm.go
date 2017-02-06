// Copyright 2016 Apcera Inc. All rights reserved.

package gcp

import (
	"errors"
	"net"
	"time"

	"github.com/apcera/libretto/ssh"
	"github.com/apcera/libretto/util"
	"github.com/apcera/libretto/virtualmachine"
)

const (
	// PublicIP represents the index of the public IP address that GetIPs returns.
	PublicIP = 0

	// PrivateIP represents the private IP address that GetIPs returns.
	PrivateIP = 1

	// OperationTimeout represents Maximum time(Second) to wait for operation ready.
	OperationTimeout = 180
)

// SSHTimeout is the maximum time to wait before failing to GetSSH. This is not
// thread-safe.
var SSHTimeout = 3 * time.Minute

var (
	// Compiler will complain if google.VM doesn't implement VirtualMachine interface.
	_ virtualmachine.VirtualMachine = (*VM)(nil)
)

// VM defines a GCE virtual machine.
type VM struct {
	Name        string
	Description string
	Zone        string
	MachineType string
	Preemptible bool // Preemptible instances will be terminates after they run for 24 hours.

	SourceImage   string   //Required
	ImageProjects []string //Required

	Disks []Disk // At least one disk is required, the first one is booted device

	Network          string
	Subnetwork       string
	UseInternalIP    bool
	PrivateIPAddress string

	Scopes  []string //Access scopes
	Project string   //GCE project
	Tags    []string //Instance Tags

	AccountFile  string
	account      accountFile
	SSHCreds     ssh.Credentials // privateKey is required for GCE
	SSHPublicKey string
}

// Disk represents the GCP Disk.
// See https://cloud.google.com/compute/docs/disks/?hl=en_US&_ga=1.115106433.702756738.1463769954
type Disk struct {
	Name       string
	DiskType   string
	DiskSizeGb int
	AutoDelete bool // Auto delete disk
}

// GetName returns the name of the virtual machine.
func (vm *VM) GetName() string {
	return vm.Name
}

// Provision creates a virtual machine on GCE. It returns an error if
// there was a problem during creation.
func (vm *VM) Provision() error {
	s, err := vm.getService()
	if err != nil {
		return err
	}

	return s.provision()
}

// GetIPs returns a slice of IP addresses assigned to the VM.
func (vm *VM) GetIPs() ([]net.IP, error) {
	s, err := vm.getService()
	if err != nil {
		return nil, err
	}

	return s.getIPs()
}

// Destroy deletes the VM on GCE.
func (vm *VM) Destroy() error {
	s, err := vm.getService()
	if err != nil {
		return err
	}

	return s.delete()
}

// GetState retrieve the instance status.
func (vm *VM) GetState() (string, error) {
	s, err := vm.getService()
	if err != nil {
		return "", err
	}

	instance, err := s.getInstance()
	if err != nil {
		return "", err
	}

	switch instance.Status {
	case "PROVISIONING", "STAGING":
		return virtualmachine.VMStarting, nil
	case "RUNNING":
		return virtualmachine.VMRunning, nil
	case "STOPPING", "STOPPED", "TERMINATED":
		return virtualmachine.VMHalted, nil
	default:
		return virtualmachine.VMUnknown, nil
	}
}

// Suspend is not supported, return the error.
func (vm *VM) Suspend() error {
	return errors.New("Suspend action not supported by GCE")
}

// Resume is not supported, return the error.
func (vm *VM) Resume() error {
	return errors.New("Resume action not supported by GCE")
}

// Halt stops a GCE instance.
func (vm *VM) Halt() error {
	s, err := vm.getService()
	if err != nil {
		return err
	}

	return s.stop()
}

// Start a stopped GCE instance.
func (vm *VM) Start() error {
	s, err := vm.getService()
	if err != nil {
		return err
	}

	return s.start()
}

// GetSSH returns an SSH client connected to the instance.
func (vm *VM) GetSSH(options ssh.Options) (ssh.Client, error) {
	ips, err := vm.GetIPs()
	if err != nil {
		return nil, err
	}

	client := &ssh.SSHClient{
		Creds:   &vm.SSHCreds,
		IP:      ips[PublicIP],
		Options: options,
		Port:    22,
	}

	if err := client.WaitForSSH(SSHTimeout); err != nil {
		return nil, err
	}

	return client, nil
}

// InsertSSHKey uploads new ssh key into the GCE instance.
func (vm *VM) InsertSSHKey(publicKey string) error {
	s, err := vm.getService()
	if err != nil {
		return err
	}

	return s.insertSSHKey()
}

// DeleteDisks cleans up all the disks attached to the GCE instance.
func (vm *VM) DeleteDisks() error {
	s, err := vm.getService()
	if err != nil {
		return err
	}

	errs := s.deleteDisks()
	if len(errs) > 0 {
		err = util.CombineErrors(": ", errs...)
		return err
	}

	return nil
}
