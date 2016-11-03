package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/sjpotter/infranetes/cmd/infranetes/flags"
	"github.com/sjpotter/infranetes/pkg/infranetes"
	"github.com/sjpotter/infranetes/pkg/infranetes/provider"

	_ "github.com/sjpotter/infranetes/pkg/infranetes/provider/aws"
	_ "github.com/sjpotter/infranetes/pkg/infranetes/provider/docker"
	_ "github.com/sjpotter/infranetes/pkg/infranetes/provider/virtualbox"
)

const (
	infranetesVersion = "0.1"
)

type BaseConfig struct {
	Cloud     string
	Container string
}

func main() {
	flag.Parse()

	if *flags.Version {
		fmt.Printf("infranetes version: %s\n", infranetesVersion)
		os.Exit(0)
	}

	if *flags.MasterIP == "" {
		fmt.Printf("Need to specify master ip address")
		os.Exit(1)
	}

	if *flags.IPBase == "" {
		fmt.Println("Need to specify an IPBase")
		os.Exit(2)
	}

	conf := BaseConfig{
		Cloud:     *flags.PodProvider,
		Container: *flags.ContProvider,
	}

	if strings.Compare("", *flags.ConfigFile) != 0 {
		file, err := ioutil.ReadFile(*flags.ConfigFile)
		if err != nil {
			fmt.Printf("File error: %v\n", err)
			os.Exit(1)
		}

		json.Unmarshal(file, &conf)
	}

	podProvider, err := provider.NewPodProvider(conf.Cloud)
	if err != nil {
		fmt.Printf("Couldn't create pod provider: %v\n", err)
		os.Exit(1)
	}

	contProvider, err := provider.NewImageProvider(conf.Container)
	if err != nil {
		fmt.Printf("Couldn't create image provider: %v\n", err)
		os.Exit(1)
	}

	server, err := infranetes.NewInfranetesManager(podProvider, contProvider)
	if err != nil {
		fmt.Println("Initialize infranetes server failed: ", err)
		os.Exit(1)
	}

	fmt.Println(server.Serve(*flags.Listen))
}
