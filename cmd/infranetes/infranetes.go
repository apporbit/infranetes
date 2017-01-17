package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/golang/glog"

	"github.com/sjpotter/infranetes/cmd/infranetes/flags"
	"github.com/sjpotter/infranetes/pkg/infranetes"
	"github.com/sjpotter/infranetes/pkg/infranetes/provider"

	//Registered Providers
	_ "github.com/sjpotter/infranetes/pkg/infranetes/provider/aws"
	_ "github.com/sjpotter/infranetes/pkg/infranetes/provider/docker"
	_ "github.com/sjpotter/infranetes/pkg/infranetes/provider/fake"
	_ "github.com/sjpotter/infranetes/pkg/infranetes/provider/gcp"
	_ "github.com/sjpotter/infranetes/pkg/infranetes/provider/virtualbox"
)

const (
	infranetesVersion = "0.1"
)

type BaseConfig struct {
	Cloud string
	Image string
}

func main() {
	flag.Parse()

	if *flags.Version {
		fmt.Printf("infranetes version: %s\n", infranetesVersion)
		os.Exit(0)
	}

	if *flags.MasterIP == "" {
		glog.Warning("Warning: MasterIP Not Set, Will try to extrapolate later")
	}

	if *flags.IPBase == "" {
		glog.Warning("Warning: IPBase Not Set, Will try to extrapolate later")
	}

	conf := BaseConfig{
		Cloud: *flags.PodProvider,
		Image: *flags.ImgProvider,
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

	imgProvider, err := provider.NewImageProvider(conf.Image)
	if err != nil {
		fmt.Printf("Couldn't create image provider: %v\n", err)
		os.Exit(1)
	}

	if !imgProvider.Integrate(podProvider) {
		fmt.Printf("%v container image provider is not compatible with %v pod provider\n", conf.Image, imgProvider)
	}

	server, err := infranetes.NewInfranetesManager(podProvider, imgProvider)
	if err != nil {
		fmt.Println("Initialize infranetes server failed: ", err)
		os.Exit(1)
	}

	fmt.Println(server.Serve(*flags.Listen))
}
