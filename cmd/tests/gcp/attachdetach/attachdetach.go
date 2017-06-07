package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/sjpotter/infranetes/pkg/infranetes/provider/gcp"
)

type gceConfig struct {
	Zone        string
	SourceImage string
	Project     string
	Scope       string
	AuthFile    string
	Network     string
	Subnet      string
}

func main() {
	var conf gceConfig

	file, err := ioutil.ReadFile("gce.json")
	if err != nil {
		fmt.Printf("File error: %v\n", err)
		os.Exit(1)
	}

	json.Unmarshal(file, &conf)

	if conf.SourceImage == "" || conf.Zone == "" || conf.Project == "" || conf.Scope == "" || conf.AuthFile == "" || conf.Network == "" || conf.Subnet == "" {
		msg := fmt.Sprintf("Failed to read in complete config file: conf = %+v", conf)
		fmt.Println(msg)
		os.Exit(1)
	}

	s, err := gcp.GetService(conf.AuthFile, conf.Project, conf.Zone, []string{conf.Scope})
	if err != nil {
		fmt.Printf("GetService failed :%v", err)
		os.Exit(1)
	}

	switch os.Args[1] {
	case "attach":
		err = s.AttachDisk(os.Args[2], os.Args[3], "testdev")
		if err != nil {
			fmt.Printf("AttachDisk failed: %v", err)
			os.Exit(1)
		}
		break
	case "detach":
		err = s.DetatchDisk(os.Args[3], "testdev")
		if err != nil {
			fmt.Printf("DetachDisk failed: %v", err)
			os.Exit(1)
		}
		break
	default:
		fmt.Printf("attach/detach?\n")
		os.Exit(1)
	}
}
