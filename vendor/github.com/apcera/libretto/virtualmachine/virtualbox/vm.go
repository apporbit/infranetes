// Copyright 2015 Apcera Inc. All rights reserved.

package virtualbox

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apcera/libretto/util"
	"github.com/apcera/util/uuid"

	libssh "github.com/apcera/libretto/ssh"
	lvm "github.com/apcera/libretto/virtualmachine"
)

// Virtualbox uses the same file name regardless of vmname when importing;
// when trying to create in parallel you will get a VERR_ALREADY_EXISTS
// error. https://github.com/mitchellh/packer/issues/1893
var createMutex = &sync.Mutex{}

// Config represents a config for a VirtualBox VM
type Config struct {
	NICs []NIC
}

// Backing represents a backing for VirtualBox NIC
type Backing int

// NIC represents a Virtualbox NIC
type NIC struct {
	Idx           int
	Backing       Backing
	BackingDevice string
}

// Runner is an encapsulation around the vmrun utility.
type Runner interface {
	Run(args ...string) (string, string, error)
	RunCombinedError(args ...string) (string, error)
}

// vboxRunner implements the Runner interface.
type vboxRunner struct {
}

var runner Runner = vboxRunner{}

// Regexp for parsing vboxmanage output.
var (
	ipLineRegexp    = regexp.MustCompile(`/VirtualBox/GuestInfo/Net/0/V4/IP`)
	ipAddrRegexp    = regexp.MustCompile(`value: .*, timestamp`)
	timestampRegexp = regexp.MustCompile(`timestamp: \d*`)
	networkRegexp   = regexp.MustCompile(`(?s)Name:.*?VBoxNetworkName`)
	stateRegexp     = regexp.MustCompile(`^State:`)
	runningRegexp   = regexp.MustCompile(`running`)
	backingRegexp   = regexp.MustCompile(`Attachment: NAT`)
	disabledRegexp  = regexp.MustCompile(`disabled$`)
	nicRegexp       = regexp.MustCompile(`^NIC \d\d?:`)
)

// Backing information for VirtualBox network cards
const (
	Nat Backing = iota
	Bridged
	Unsupported
	Disabled
)

// VM represents a VirtualBox VM
type VM struct {
	Src         string
	ips         []net.IP
	Credentials libssh.Credentials
	Name        string
	Config      Config
	ipUpdate    map[string]string
}

// GetName returns the name of the virtual machine
func (vm *VM) GetName() string {
	return vm.Name
}

// GetSSH returns an ssh client for the the VM.
func (vm *VM) GetSSH(options libssh.Options) (libssh.Client, error) {
	ips, err := util.GetVMIPs(vm, options)
	if err != nil {
		return nil, err
	}
	vm.ips = ips

	client := libssh.SSHClient{Creds: &vm.Credentials, IP: ips[0], Port: 22, Options: options}
	return &client, nil
}

// Destroy powers off the VM and deletes its files from disk.
func (vm *VM) Destroy() error {
	err := vm.Halt()
	if err != nil {
		return err
	}

	// vbox will not release it's lock immediately after the stop
	time.Sleep(1 * time.Second)

	_, err = runner.RunCombinedError("unregistervm", vm.Name, "--delete")
	if err != nil {
		return lvm.WrapErrors(lvm.ErrDeletingVM, err)
	}
	return nil
}

// Halt powers off the VM without destroying it
func (vm *VM) Halt() error {
	state, err := vm.GetState()
	if err != nil {
		return err
	}
	if state == lvm.VMHalted {
		return nil
	}
	_, err = runner.RunCombinedError("controlvm", vm.Name, "poweroff")
	if err != nil {
		return lvm.WrapErrors(lvm.ErrStoppingVM, err)
	}
	return nil
}

// Start powers on the VM
func (vm *VM) Start() error {
	_, err := runner.RunCombinedError("startvm", vm.Name)
	if err != nil {
		// If the user has paused the VM it reads as halted but the Start
		// command will fail. Try to resume it as a backup.
		_, rerr := runner.RunCombinedError("controlvm", vm.Name, "resume")
		if rerr != nil {
			// If neither succeeds, return both errors.
			return lvm.WrapErrors(lvm.ErrStartingVM, err, rerr)
		}
	}
	return nil
}

// Suspend suspends the active state of the VM.
func (vm *VM) Suspend() error {
	_, err := runner.RunCombinedError("controlvm", vm.Name, "savestate")
	if err != nil {
		return lvm.WrapErrors(lvm.ErrSuspendingVM, err)
	}
	return nil
}

// Resume restarts the active state of the VM.
func (vm *VM) Resume() error {
	return vm.Start()
}

// GetIPs returns a list of ip addresses associated with the vm through VBox Guest Additions.
func (vm *VM) GetIPs() ([]net.IP, error) {
	vm.waitUntilReady()

	return vm.ips, nil
}

// GetState gets the power state of the VM being serviced by this driver.
func (vm *VM) GetState() (string, error) {
	stdout, err := runner.RunCombinedError("showvminfo", vm.Name)
	if err != nil {
		return "", lvm.WrapErrors(lvm.ErrVMInfoFailed, err)
	}
	for _, line := range strings.Split(stdout, "\n") {
		// See if this is a NIC
		if match := stateRegexp.FindStringSubmatch(line); match != nil {
			if match := runningRegexp.FindStringSubmatch(line); match != nil {
				return lvm.VMRunning, nil
			}
			return lvm.VMHalted, nil
		}
	}
	return lvm.VMUnknown, lvm.ErrVMStateFailed
}

// GetInterfaces gets all the network cards attached to this VM
func (vm *VM) GetInterfaces() ([]NIC, error) {
	nics := []NIC{}
	stdout, err := runner.RunCombinedError("showvminfo", vm.Name)
	if err != nil {
		return nil, err
	}

	for _, line := range strings.Split(stdout, "\n") {
		// See if this is a NIC
		if match := nicRegexp.FindStringSubmatch(line); match != nil {
			var nic NIC
			// Get the nic index and the backing
			// substr is the nic index with a trailing `:`
			substr := strings.Split(match[0], " ")[1]
			//Remove the trailing `:` and convert to an integer
			idx, err := strconv.Atoi(strings.TrimSuffix(substr, ":"))
			if err != nil {
				return nil, err
			}
			nic.Idx = idx
			if match := backingRegexp.FindStringSubmatch(line); match != nil {
				nic.Backing = Nat
			} else if match := disabledRegexp.FindStringSubmatch(line); match != nil {
				nic.Backing = Disabled
			} else {
				nic.Backing = Unsupported
			}
			nics = append(nics, nic)
		}
	}
	return nics, nil
}

// Provision imports the VM and waits until it is booted up.
func (vm *VM) Provision() error {
	var name string
	if vm.Name == "" {
		name = fmt.Sprintf("vm-%s", uuid.Variant4())
		vm.Name = name
	}

	src := vm.Src
	if src == "" {
		return lvm.ErrSourceNotSpecified
	}
	ovaPath, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	vm.Src = ovaPath

	// See comment on mutex definition for details.
	createMutex.Lock()
	_, err = runner.RunCombinedError("import", vm.Src, "--vsys", "0", "--vmname", vm.Name)
	createMutex.Unlock()
	if err != nil {
		return err
	}

	err = vm.configure()
	if err != nil {
		return err
	}

	return vm.waitUntilReady()
}

// Run runs a VBoxManage command.
func (f vboxRunner) Run(args ...string) (string, string, error) {
	var vboxManagePath string
	// If vBoxManage is not found in the system path, fall back to the
	// hard coded path.
	if path, err := exec.LookPath("VBoxManage"); err == nil {
		vboxManagePath = path
	} else {
		vboxManagePath = VBOXMANAGE
	}
	cmd := exec.Command(vboxManagePath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// RunCombinedError runs a VBoxManage command.  The output is stdout and the the
// combined err/stderr from the command.
func (f vboxRunner) RunCombinedError(args ...string) (string, error) {
	wout, werr, err := f.Run(args...)
	if err != nil {
		if werr != "" {
			return wout, fmt.Errorf("%s: %s", err, werr)
		}
		return wout, err
	}

	return wout, nil
}
