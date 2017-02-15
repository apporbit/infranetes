package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"time"

	//	"github.com/golang/glog"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"github.com/sjpotter/infranetes/pkg/common"
	fv "k8s.io/kubernetes/pkg/volume/flexvolume"
	"strconv"
)

// FIXME: make these configurable from a configuration file
var (
	// Iso is location for the fake mount to loopback from, can switch this to a smaller squashfs file, unsure makes a huge difference
	Iso string = "/tmp/test.iso"

	// InfraSocket is the location for the infrantes socket
	InfraSocket string = "/tmp/infra"

	kubelexPrefix = "/var/lib/kubelet/"
)

var (
	client common.MountsClient
)

// FIXME: can't handle attach like this, as doesn't return "device"
func retOk() {
	status := fv.FlexVolumeDriverStatus{Status: fv.StatusSuccess}
	b, _ := json.Marshal(status)

	fmt.Printf("%v", string(b))

	//	glog.Flush()
	os.Exit(0)
}

func retErr(s, m string) {
	status := fv.FlexVolumeDriverStatus{Status: s, Message: m}
	b, _ := json.Marshal(status)

	fmt.Fprintf(os.Stderr, "%v", string(b))
	//	glog.Infof("%+v", os.Args)
	//	glog.Infof("%v", m)

	//	glog.Flush()
	os.Exit(1)
}

func do_init() {
	// FIXME: make sure infranetes supports us, possibly clear out table?

	retOk()
}

func do_mount(mntpnt, flexOpts string) {

	// 0. json string to map
	j := []byte(flexOpts)
	opts := map[string]string{}
	e := json.Unmarshal(j, &opts)
	if e != nil {
		retErr(fv.StatusFailure, "do_mount: Failed to unmarshall flexopts")
	}

	// 1. Figure out what pod we are mounting for
	regex := kubelexPrefix + "pods/([0-9a-f-]*)/"

	re := regexp.MustCompile(regex)
	matches := re.FindStringSubmatch(mntpnt)
	if len(matches) != 2 {
		msg := fmt.Sprintf("do_mount: couldn't find pod uuid, mntpnt = %v, regex = %v", mntpnt, regex)
		retErr(fv.StatusFailure, msg)
	}

	// 2. Extract config details
	vol := opts["volumeID"]
	if vol == "" {
		retErr(fv.StatusFailure, "do_mount: No volumeID")
	}
	dev := opts["device"]
	fsType := opts["kubernetes.io/fsType"]
	if fsType == "" {
		fsType = "ext4"
	}

	ro := false
	switch opts["kubernetes.io/readwrite"] {
	case "rw":
		ro = false
	case "ro":
		ro = true
	}

	m := opts["mount"]
	mount, e := strconv.ParseBool(m)
	if e == nil && !mount {
		mntpnt = ""
	}

	vmMntPnt := mntpnt
	// If deviceOnly is true, won't be mounted in the VM, as won't be used inside a container as the file system,
	// just available as a device to the VM
	if d, ok := opts["deviceOnly"]; ok {
		if b, err := strconv.ParseBool(d); err == nil {
			if b {
				vmMntPnt = ""
			}
		}
	}

	req := &common.AddMountRequest{
		Volume:     vol,
		MountPoint: vmMntPnt,
		FsType:     fsType,
		Device:     dev,
		ReadOnly:   ro,
		PodUUID:    matches[1],
	}

	// 3. Tell infrantes about this mount
	_, e = client.AddMount(context.Background(), req)
	if e != nil {
		retErr(fv.StatusFailure, fmt.Sprintf("do_mount: Failuring adding to Infranetes: %v", e))
	}

	// 4. mount the pseudo mount on infranetes node so kubelet plays nice
	// Apparently kubelet doesn't create the mountpoint.
	os.MkdirAll(mntpnt, 0755)

	cmd := exec.Command("mount", "-o", "loop", Iso, mntpnt)
	output, e := cmd.CombinedOutput()
	if e != nil {
		client.DelMount(context.Background(), &common.DelMountRequest{MountPoint: mntpnt})
		msg := fmt.Sprintf("do_mount: mntpnt: %v, flexOpts: %v: output %v", mntpnt, flexOpts, string(output))
		retErr(fv.StatusFailure, msg)
	}

	retOk()
}

func do_umount(mntpnt string) {
	// FIXME: unsure DelMount matters if volumes in a pod aren't mutable
	_, e := client.DelMount(context.Background(), &common.DelMountRequest{MountPoint: mntpnt})
	if e != nil {
		//		glog.Infof("do_umount: Failure removing from Infranetes")
	}

	cmd := exec.Command("umount", mntpnt)
	output, e := cmd.CombinedOutput()
	if e != nil {
		msg := fmt.Sprintf("do_umount: mntpnt: %v, output: %v", mntpnt, string(output))
		retErr(fv.StatusFailure, msg)
	}

	retOk()
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
		retErr(fv.StatusFailure, "Not Enough Arguments")
	}

	grpcClient, e := dial(InfraSocket)
	if e != nil {
		retErr(fv.StatusFailure, "Failed to connect to Infranetes")
	}

	client = common.NewMountsClient(grpcClient)

	switch os.Args[1] {
	case "init":
		do_init()
	case "attach":
		//retErr(fv.StatusNotSupported, "Ignoring attach-detach for now")
		retErr(fv.StatusNotSupported, "")
	case "detach":
		//retErr(fv.StatusNotSupported, "Ignoring attach-detach for now")
		retErr(fv.StatusNotSupported, "")
	case "mount":
		if len(os.Args) != 5 {
			retErr(fv.StatusFailure, "mount needs 3 arguments")
		}
		do_mount(os.Args[2], os.Args[4])
	case "unmount":
		if len(os.Args) != 3 {
			retErr(fv.StatusFailure, "unmount needs 1 arguments")
		}

		do_umount(os.Args[2])
	default:
		retErr(fv.StatusFailure, fmt.Sprintf("Unknown Command: %v", os.Args[1]))
	}
}
