// Copyright 2015 Apcera Inc. All rights reserved.

package virtualbox

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	lvm "github.com/apcera/libretto/virtualmachine"
)

type ifKeyValue struct {
	k, v string
}

// getBridgedDeviceKV returns a list of key and value pairs of chosen key and value names
// from VBoxManage list bridgedifs results.
func getBridgedDeviceKV(keyname, propname string) ([]ifKeyValue, error) {
	kvs := []ifKeyValue{}
	if keyname == "" || propname == "" {
		return kvs, nil
	}

	stdout, _, err := runner.Run("list", "bridgedifs")
	if err != nil {
		return kvs, err
	}
	matches := networkRegexp.FindAllString(stdout, -1)
	if len(matches) < 1 {
		return kvs, nil
	}

	keyid := strings.TrimSuffix(keyname, ":") + ":"
	propid := strings.TrimSuffix(propname, ":") + ":"
	// Each match is a device
	for _, device := range matches {
		var kv ifKeyValue
		// Find the record that contains keyname and retrieve propname value.
		for _, line := range strings.Split(device, "\n") {
			if strings.Contains(line, keyid) {
				kv.k = strings.TrimSpace(strings.TrimPrefix(line, keyid))
			} else if strings.Contains(line, propid) {
				kv.v = strings.TrimSpace(strings.TrimPrefix(line, propid))
			}
			if kv.k != "" && kv.v != "" {
				kvs = append(kvs, kv)
				break
			}
		}
	}
	return kvs, nil
}

// GetBridgedDeviceNameIPMap returns a map of network device
// name and its IP address from the list of bridgedifs reported
// by VirtualBox manager.
func GetBridgedDeviceNameIPMap() (map[string]string, error) {
	m := map[string]string{}
	kvs, err := getBridgedDeviceKV("Name", "IPAddress")
	if err != nil {
		return m, err
	}
	for _, kv := range kvs {
		if kv.k != "" {
			m[kv.k] = kv.v
		}
	}
	return m, nil
}

// GetBridgedDeviceName takes the mac address of a network device as a string
// and returns the name of the network device corresponding to it as seen by
// Virtualbox
func GetBridgedDeviceName(macAddr string) (string, error) {
	stdout, _, err := runner.Run("list", "bridgedifs")
	if err != nil {
		return "", err
	}
	if matches := networkRegexp.FindAllString(stdout, -1); len(matches) > 0 {
		// Each match is a device
		for _, device := range matches {
			var mac, name string
			// Find the mac address and the name
			for _, line := range strings.Split(device, "\n") {
				if strings.Contains(line, "HardwareAddress") {
					mac = strings.TrimSpace(strings.TrimPrefix(line, "HardwareAddress:"))
				}
				if strings.Contains(line, "Name:") {
					name = strings.TrimSpace(strings.TrimPrefix(line, "Name:"))
				}
			}
			if strings.Contains(macAddr, mac) {
				return name, nil
			}
		}
	}
	return "", nil
}

// GetBridgedDevices returns a slice of network devices that can be connected to VMs in bridged mode
func GetBridgedDevices() ([]string, error) {
	deviceNames := []string{}
	ifAndMac, err := getBridgedDeviceKV("Name", "HardwareAddress")
	if err != nil {
		return deviceNames, err
	}
	for _, kv := range ifAndMac {
		if kv.k != "" && kv.v != "" {
			deviceNames = append(deviceNames, kv.k)
		}
	}
	return deviceNames, nil
}

func (vm *VM) configure() error {
	// Delete any existing nics from the VM, will add the network cards from the passed in config
	if err := DeleteNICs(*vm); err != nil {
		return err
	}

	for _, nic := range vm.Config.NICs {
		if err := AddNIC(*vm, nic); err != nil {
			return err
		}
	}
	return nil
}

// This function makes a single request to get IPs from a VM.
func (vm *VM) requestIPs() []net.IP {
	if vm.ipUpdate == nil {
		vm.ipUpdate = map[string]string{}
	}
	var ips []net.IP
	stdout, _, _ := runner.Run("guestproperty", "enumerate", vm.Name)
	for _, line := range strings.Split(stdout, "\n") {
		if match := ipLineRegexp.FindStringSubmatch(line); match != nil {
			if match := ipAddrRegexp.FindStringSubmatch(line); match != nil {
				ipString := strings.Split(match[0], ":")[1]
				ipString = strings.TrimSuffix(strings.TrimSpace(ipString), ", timestamp")
				if ip := net.ParseIP(ipString); ip != nil {
					ips = append(ips, ip)
					if match := timestampRegexp.FindStringSubmatch(line); match != nil {
						vm.ipUpdate[ipString] = match[0]
					}
				}
			}
		}
	}
	vm.ips = ips
	return ips
}

func (vm *VM) waitUntilReady() error {
	// Check if the vm already has ips before starting the vm
	// If it does then wait until the timestamp for at least one of them changes.
	var ips []net.IP
	ips = vm.requestIPs()
	timestamps := map[string]string{}
	for k, v := range vm.ipUpdate {
		timestamps[k] = v
	}

	err := vm.Start()
	if err != nil {
		return err
	}
	quit := make(chan bool, 1)
	success := make(chan bool, 1)
	timer := time.NewTimer(time.Second * 90)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-quit:
				success <- false
				return
			default:
				ips = vm.requestIPs()
				if len(ips) == 0 {
					time.Sleep(2 * time.Second)
					continue
				} else {
					// Check if the timestamps have changed
					for k, v := range vm.ipUpdate {
						// Check if the key even existed before, if it is a new key then all is good
						timestamp, ok := timestamps[k]
						if !ok {
							success <- true
							return
						}
						// If it is not a new key, then check if the timestamp is updated
						if timestamp != v {
							success <- true
							return
						}
					}
				}
			}
		}
	}()
	var r error
OuterLoop:
	for {
		select {
		case s := <-success:
			if !s {
				r = lvm.ErrVMNoIP
			}
			timer.Stop()
			break OuterLoop
		case <-timer.C:
			quit <- true
			r = lvm.ErrVMBootTimeout
			break OuterLoop

		}
	}
	wg.Wait()
	return r
}

// DeleteNIC deletes the specified network interface on the vm.
func DeleteNIC(vm VM, nic NIC) error {
	if nic.Backing == Disabled {
		return lvm.ErrNICAlreadyDisabled
	}
	_, _, err := runner.Run("modifyvm", vm.Name, fmt.Sprintf("--nic%d", nic.Idx), "null")
	return err
}

func getStringFromBacking(backing Backing) string {
	switch backing {
	case Nat:
		return "nat"
	case Bridged:
		return "bridged"
	}
	return "null"
}

// AddNIC adds a NIC to the VM.
func AddNIC(vm VM, nic NIC) error {
	var err error
	switch nic.Backing {
	case Nat:
		_, _, err = runner.Run("modifyvm", vm.Name, fmt.Sprintf("--nic%d", nic.Idx), getStringFromBacking(nic.Backing))
	case Bridged:
		_, _, err = runner.Run("modifyvm", vm.Name, fmt.Sprintf("--nic%d", nic.Idx), getStringFromBacking(nic.Backing), fmt.Sprintf("--bridgeadapter%d", nic.Idx), nic.BackingDevice)
	}
	return err
}

// DeleteNICs disables all the network interfaces on the vm.
func DeleteNICs(vm VM) error {
	nics, err := vm.GetInterfaces()
	if err != nil {
		return lvm.ErrFailedToGetNICS
	}
	for _, nic := range nics {
		if nic.Backing != Disabled {
			err := DeleteNIC(vm, nic)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
