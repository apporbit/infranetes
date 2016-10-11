package main

import (
	"flag"
	"fmt"
	"github.com/apcera/libretto/ssh"
	"github.com/apcera/libretto/virtualmachine/aws"
	"io/ioutil"
	"path/filepath"
	"strings"
)

var (
	key = flag.String("key", "", "keypair to use for ec2")
)

func main() {
	flag.Parse()

	state, err := vm.GetState()
	if err != nil {
		fmt.Printf("Failed to get vm state: %v\n", err)
		return
	}
	fmt.Printf("vm state = %v\n", state)

	ips, err := vm.GetIPs()
	if err != nil {
		fmt.Printf("failed to get vm ips: %v\n", err)
		return
	}
	fmt.Printf("VM ips = %+v\n", ips)

	err = vm.Halt()
	if err != nil {
		fmt.Printf("failed to halt vm: %v\n", err)
		return
	}

	err = vm.Destroy()
	if err != nil {
		fmt.Printf("failed to destroy vm: %v\n", err)
	}
}
