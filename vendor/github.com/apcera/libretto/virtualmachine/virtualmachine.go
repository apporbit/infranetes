// Copyright 2015 Apcera Inc. All rights reserved.

package virtualmachine

import (
	"errors"
	"net"
	"strings"

	"github.com/apcera/libretto/ssh"
)

// VirtualMachine represents a VM which can be provisioned using this library.
type VirtualMachine interface {
	GetName() string
	Provision() error
	GetIPs() ([]net.IP, error)
	Destroy() error
	GetState() (string, error)
	Suspend() error
	Resume() error
	Halt() error
	Start() error
	GetSSH(ssh.Options) (ssh.Client, error)
}

const (
	// VMStarting is the state to use when the VM is starting
	VMStarting = "starting"
	// VMRunning is the state to use when the VM is running
	VMRunning = "running"
	// VMHalted is the state to use when the VM is halted or shutdown
	VMHalted = "halted"
	// VMSuspended is the state to use when the VM is suspended
	VMSuspended = "suspended"
	// VMPending is the state to use when the VM is waiting for action to complete
	VMPending = "pending"
	// VMError is the state to use when the VM is in error state
	VMError = "error"
	// VMUnknown is the state to use when the VM is unknown state
	VMUnknown = "unknown"
)

var (
	// ErrVMNoIP is returned when a newly provisoned VM does not get an IP address.
	ErrVMNoIP = errors.New("error getting a new IP for the virtual machine")

	// ErrVMBootTimeout is returned when a timeout occurs waiting for a vm to boot.
	ErrVMBootTimeout = errors.New("timed out waiting for virtual machine")

	// ErrNICAlreadyDisabled is returned when a NIC we are trying to disable is already disabled.
	ErrNICAlreadyDisabled = errors.New("NIC already disabled")

	// ErrFailedToGetNICS is returned when no NICS can be found on the vm
	ErrFailedToGetNICS = errors.New("failed to get interfaces for vm")

	// ErrStartingVM is returned when the VM cannot be started
	ErrStartingVM = errors.New("error starting VM")

	// ErrCreatingVM is returned when the VM cannot be created
	ErrCreatingVM = errors.New("error creating VM")

	// ErrStoppingVM is returned when the VM cannot be stopped
	ErrStoppingVM = errors.New("error stopping VM")

	// ErrDeletingVM is returned when the VM cannot be deleted
	ErrDeletingVM = errors.New("error deleting VM")

	// ErrVMInfoFailed is returned when the VM cannot be deleted
	ErrVMInfoFailed = errors.New("error getting information about VM")

	// ErrVMStateFailed is returned when no state can be parsed for the VM
	ErrVMStateFailed = errors.New("error getting the state of the VM")

	// ErrSourceNotSpecified is returned when no source is specified for the VM
	ErrSourceNotSpecified = errors.New("source not specified")

	// ErrDestNotSpecified is returned when no destination is specified for the VM
	ErrDestNotSpecified = errors.New("source not specified")

	// ErrSuspendingVM is returned when the VM cannot be suspended
	ErrSuspendingVM = errors.New("error suspending the VM")

	// ErrResumingVM is returned when the VM cannot be resumed
	ErrResumingVM = errors.New("error resuming the VM")

	// ErrNotImplemented is returned when the operation is not implemented
	ErrNotImplemented = errors.New("operation not implemented")

	// ErrSuspendNotSupported is returned when vm.Suspend() is called, but not supported.
	ErrSuspendNotSupported = errors.New("suspend action not supported")

	// ErrResumeNotSupported is returned when vm.Resume() is called, but not supported.
	ErrResumeNotSupported = errors.New("resume action not supported")
)

// WrapErrors squashes multiple errors into a single error, separated by ": ".
func WrapErrors(errs ...error) error {
	s := []string{}
	for _, e := range errs {
		if e != nil {
			s = append(s, e.Error())
		}
	}
	return errors.New(strings.Join(s, ": "))
}
