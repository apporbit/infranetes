// Copyright 2015 Apcera Inc. All rights reserved.

package util

import (
	"fmt"
	"math/rand"
	"net"
	"strings"
	"time"

	"github.com/apcera/libretto/ssh"
	lvm "github.com/apcera/libretto/virtualmachine"
)

// Random generates a random number in between min and max
// If min equals max then min is returned. If max is less than min
// then the function panics.
func Random(min, max int) int {
	if min == max {
		return min
	}
	if max < min {
		panic("max cannot be less than min")
	}
	rand.Seed(time.Now().UTC().UnixNano())
	return rand.Intn(max-min+1) + min
}

// GetVMIPs returns the IPs associated with the given VM. If the IPs are present
// in options, they will be returned. Otherwise, an API call will be made to
// get the list of IPs. An error is returned if the API call fails or returns
// nothing.
func GetVMIPs(vm lvm.VirtualMachine, options ssh.Options) ([]net.IP, error) {
	ips := options.IPs
	if len(ips) == 0 {
		var err error
		ips, err = vm.GetIPs()
		if err != nil {
			return nil, fmt.Errorf("Error getting IPs for the VM: %s", err)
		}
		if len(ips) == 0 {
			return nil, lvm.ErrVMNoIP
		}
	}
	return ips, nil
}

// CombineErrors converts all the errors from slice into a single error
func CombineErrors(delimiter string, errs ...error) error {
	var formatStrs = []string{}

	for _, e := range errs {
		if e == nil {
			continue
		}
		formatStrs = append(formatStrs, e.Error())
	}

	return fmt.Errorf(strings.Join(formatStrs, delimiter))
}
