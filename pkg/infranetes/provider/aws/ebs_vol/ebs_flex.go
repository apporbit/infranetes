package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"time"

	"github.com/golang/glog"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"github.com/sjpotter/infranetes/pkg/common"
)

const (
	// StatusSuccess represents the successful completion of command.
	StatusSuccess = "Success"
	// StatusFailed represents that the command failed.
	StatusFailure = "Failed"
	// StatusNotSupported represents that the command is not supported.
	StatusNotSupported = "Not supported"
)

var (
	// FIXME: make these configurable from a configuration file
	// Iso is location for the fake mount to loopback from
	Iso string = "/tmp/test.iso"

	// InfraSocket is the location for the infrantes socket
	InfraSocket string = "/tmp/infra"

	client common.MountsClient
)

// FIXME: can't handle attach like this, as doesn't return "device"
func ok() {
	fmt.Printf("{%q: %q}", "status", StatusSuccess)
	glog.Flush()
	os.Exit(0)
}

func err(s, m string) {
	fmt.Fprintf(os.Stderr, "{%q: %q, %q: %q}", "status", s, "message", m)
	glog.Flush()
	os.Exit(1)
}

func do_init() {
	// FIXME: make sure infranetes supports us, possibly clear out table?

	ok()
}

func do_mount(mntpnt, flexOpts string) {
	j := []byte(flexOpts)
	opts := map[string]string{}
	e := json.Unmarshal(j, &opts)
	if e != nil {
		err(StatusFailure, "Failed to unmarshall flexopts")
	}

	vol := opts["volumeID"]

	if vol == "" {
		err(StatusFailure, "No volumeID")
	}

	// FIXME: here we would communicate with intfrantes to log this info
	_, e = client.AddMount(context.Background(), &common.AddMountRequest{Volume: vol, MountPoint: mntpnt})
	if e != nil {
		err(StatusFailure, fmt.Sprintf("Failuring adding to Infranetes: %v", e))
	}

	os.MkdirAll(mntpnt, 0755)

	cmd := exec.Command("mount", "-o", "loop", Iso, mntpnt)
	output, e := cmd.CombinedOutput()
	if e != nil {
		client.DelMount(context.Background(), &common.DelMountRequest{MountPoint: mntpnt})
		glog.Infof("do_mount: mntpnt: %v, flexOpts: %v: output %v", mntpnt, flexOpts, string(output))
		err(StatusFailure, "Fake mount Failed")
	}

	ok()
}

func do_umount(mntpnt string) {
	_, e := client.DelMount(context.Background(), &common.DelMountRequest{MountPoint: mntpnt})
	if e != nil {
		glog.Infof("do_mount: Failure removing from Infranetes")
	}

	cmd := exec.Command("umount", mntpnt)
	output, e := cmd.CombinedOutput()
	if e != nil {
		glog.Infof("do_umount: mntpnt: %v, output: %v", mntpnt, string(output))
		err(StatusFailure, "Umount Failed")
	}

	ok()
}

func dial(file string) (*grpc.ClientConn, error) {
	conn, e := grpc.Dial(file, grpc.WithInsecure(), grpc.WithTimeout(5*time.Second),
		grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
			return net.DialTimeout("unix", addr, timeout)
		}))

	if e != nil {
		return nil, fmt.Errorf("failed to connect: %v", e)
	}

	return conn, nil
}

func main() {
	flag.Parse()

	if len(os.Args) <= 1 {
		err(StatusFailure, "Not Enough Arguments")
	}

	grpcClient, e := dial(InfraSocket)
	if e != nil {
		err(StatusFailure, "Failed to connect to Infranetes")
	}

	client = common.NewMountsClient(grpcClient)

	switch os.Args[1] {
	case "init":
		do_init()
	case "attach":
		err(StatusNotSupported, "Ignoring attach/detach for now")
	case "detach":
		err(StatusNotSupported, "Ignoring attach/detach for now")
	case "mount":
		if len(os.Args) != 5 {
			err(StatusFailure, "mount needs 3 arguments")
		}
		do_mount(os.Args[2], os.Args[4])
	case "unmount":
		if len(os.Args) != 3 {
			err(StatusFailure, "unmount needs 1 arguments")
		}

		do_umount(os.Args[2])
	default:
		err(StatusFailure, fmt.Sprintf("Unknown Command: %v", os.Args[1]))
	}

	glog.Flush()
}
