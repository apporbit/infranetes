package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/sjpotter/infranetes/cmd/vmserver/flags"
	"github.com/sjpotter/infranetes/pkg/vmserver"

	_ "github.com/sjpotter/infranetes/pkg/vmserver/docker"
	_ "github.com/sjpotter/infranetes/pkg/vmserver/fake"
)

const (
	infranetesVersion = "0.1"
)

func main() {
	flag.Parse()

	if *flags.Version {
		fmt.Printf("infranetes version: %s\n", infranetesVersion)
		os.Exit(0)
	}

	contProvider, err := vmserver.NewContainerProvider(flags.ContProvider)
	if err != nil {
		fmt.Printf("Couldn't create image provider: %v\n", err)
		os.Exit(1)
	}

	server, err := vmserver.NewVMServer(flags.Cert, flags.Key, contProvider)
	if err != nil {
		fmt.Println("Initialize infranetes vm server failed: ", err)
		os.Exit(1)
	}

	fmt.Println(server.Serve(*flags.Listen))
}
