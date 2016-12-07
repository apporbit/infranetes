package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"

	"github.com/coreos/go-systemd/unit"
)

func main() {
	if len(os.Args) != 4 {
		fmt.Println("Usage:\n\tdaemon <Description> <path to binary to daemonize> <unit to write")
		os.Exit(1)
	}

	myUnit := []*unit.UnitOption{
		{Section: "Unit", Name: "Description", Value: os.Args[1]},
		{Section: "Service", Name: "ExecStart", Value: os.Args[2]},
		{Section: "Service", Name: "Restart", Value: "always"},
		{Section: "Service", Name: "RestartSec", Value: "2s"},
		{Section: "Service", Name: "StartLimitInterval", Value: "0"},
		{Section: "Service", Name: "KillMode", Value: "process"},
	}

	reader := unit.Serialize(myUnit)
	outBytes, err := ioutil.ReadAll(reader)
	if err != nil {
		fmt.Errorf("ioutil.ReadAll failed: %v\n", err)
		os.Exit(1)
	}

	ioutil.WriteFile("/run/systemd/system/"+os.Args[3]+".service", outBytes, 0644)

	cmd := exec.Command("systemctl", "daemon-reload")
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Errorf("cmd %v failed to run: %v\n", cmd, output)
		os.Exit(1)
	}
}
