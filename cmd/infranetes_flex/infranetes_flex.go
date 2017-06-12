package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"github.com/apporbit/infranetes/pkg/common"
	"github.com/apporbit/infranetes/pkg/infranetes_flex"
	fv "k8s.io/kubernetes/pkg/volume/flexvolume"

	_ "github.com/apporbit/infranetes/pkg/infranetes_flex/providers"
)

type flexConfig struct {
	// Iso is location for the fake mount to loopback from, can switch this to a smaller squashfs file, unsure makes a huge difference
	// Iso string = "/tmp/test.iso"
	Iso string
	// InfraSocket is the location for the infrantes socket
	// InfraSocket string = "/tmp/infra"
	InfraSocket string
	// Used to determine which pod uuid this belongs to
	// kubelexPrefix = "/var/lib/kubelet/"
	KubeletPrefix string
	// Free device for provisioning formatting purposes
	// formatDevice = "/dev/xvdz"
	formatDevice string
	// Used to determine which cloud specifc DevProvider to use
	DevProvider string
}

var (
	config      flexConfig
	devProvider infranetes_flex.DevProvider
	infraClient common.MountsClient
)

const (
	logFile = "/tmp/flex.log"
)

// FIXME: can't handle attach like this, as doesn't return "device"
func retOk() {
	status := fv.DriverStatus{Status: fv.StatusSuccess}
	b, _ := json.Marshal(status)
	append(fmt.Sprintf("%v", string(b)))

	fmt.Printf("%v", string(b))

	os.Exit(0)
}

func retVolumeName(v string) {
	status := fv.DriverStatus{Status: fv.StatusSuccess, VolumeName: v}
	b, _ := json.Marshal(status)
	append(fmt.Sprintf("%v", string(b)))

	fmt.Printf("%v", string(b))

	os.Exit(0)
}

func retErr(s, m string) {
	status := fv.DriverStatus{Status: s, Message: m}
	b, _ := json.Marshal(status)
	append(fmt.Sprintf("%v", string(b)))

	fmt.Fprintf(os.Stderr, "%v", string(b))

	os.Exit(1)
}

func parseConfig() error {
	file, err := ioutil.ReadFile("/root/flex.conf")
	if err != nil {
		return fmt.Errorf("File error: %v\n", err)
	}

	json.Unmarshal(file, &config)

	if config.Iso == "" || config.InfraSocket == "" || config.KubeletPrefix == "" {
		return fmt.Errorf("Couldn't parse config file, neccessary options missing")
	}

	devProvider, err = infranetes_flex.NewDevProvider(config.DevProvider)
	if err != nil {
		return err
	}

	return nil
}

func do_init() {
	// FIXME: make sure infranetes supports us, possibly clear out table?

	retOk()
}

func do_volume(flexopts string) {
	j := []byte(flexopts)
	opts := map[string]string{}
	e := json.Unmarshal(j, &opts)
	if e != nil {
		retErr(fv.StatusFailure, "do_volume: Failed to unmarshall flexopts")
	}

	if _, ok := opts["volumeID"]; !ok {
		retErr(fv.StatusFailure, "do_volume: no volumeID in flexopts")
	}

	retVolumeName(opts["volumeID"])
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
	regex := config.KubeletPrefix + "pods/([0-9a-f-]*)/"

	re := regexp.MustCompile(regex)
	matches := re.FindStringSubmatch(mntpnt)
	if len(matches) != 2 {
		retErr(fv.StatusFailure, fmt.Sprintf("do_mount: couldn't find pod uuid, mntpnt = %v, regex = %v", mntpnt, regex))
	}

	// 2. Extract config details
	dev := opts["device"]
	fsType := opts["kubernetes.io/fsType"]
	if fsType == "" {
		fsType = "ext4"
	}

	vol := opts["volumeID"]
	if vol == "" {
		_, ok := opts["size"]
		if !ok {
			retErr(fv.StatusFailure, "do_mount: No volumeID and no provisionable size")
		}

		size, err := strconv.ParseUint(opts["size"], 10, 64)
		if err != nil {
			retErr(fv.StatusFailure, fmt.Sprintf("do_mount: %v not parsable as a uint", opts["size"]))
		}

		newVol, err := provision(size)
		if err != nil {
			retErr(fv.StatusFailure, fmt.Sprintf("do_mount: failed to provision volume: %v", err))
		}

		vol = *newVol
		opts["format"] = "true"
	}

	if f, ok := opts["format"]; ok {
		format, err := strconv.ParseBool(f)
		if err == nil && format {
			do_format(&vol, &fsType)
		}
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
	_, e = infraClient.AddMount(context.Background(), req)
	if e != nil {
		retErr(fv.StatusFailure, fmt.Sprintf("do_mount: Failuring adding to Infranetes: %v", e))
	}

	// 4. mount the pseudo mount on infranetes node so kubelet plays nice
	// Apparently kubelet doesn't create the mountpoint.
	os.MkdirAll(mntpnt, 0755)

	cmd := exec.Command("mount", "-o", "loop", config.Iso, mntpnt)
	output, e := cmd.CombinedOutput()
	if e != nil {
		infraClient.DelMount(context.Background(), &common.DelMountRequest{MountPoint: mntpnt})
		msg := fmt.Sprintf("do_mount: mntpnt: %v, flexOpts: %v: output %v", mntpnt, flexOpts, string(output))
		retErr(fv.StatusFailure, msg)
	}

	retOk()
}

func provision(size uint64) (*string, error) {
	return devProvider.Provision(size)
}

func do_format(vol, fsType *string) error {
	dev, err := devProvider.Attach(vol)
	if err != nil {
		return fmt.Errorf("do_format: attach failed: %v", err)
	}

	switch *fsType {
	case "ext4":
		cmd := exec.Command("mkfs.ext4", *dev)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("do_format: mkfs.ext4 failed: %v", string(output))
		}
	default:
		return fmt.Errorf("do_format: unknown fstype: %v", *fsType)
	}

	if err := devProvider.Detach(vol); err != nil {
		return fmt.Errorf("do_format: detach failed: %v", err)
	}

	return nil
}

func do_umount(mntpnt string) {
	// FIXME: unsure DelMount matters if volumes in a pod aren't mutable
	_, e := infraClient.DelMount(context.Background(), &common.DelMountRequest{MountPoint: mntpnt})
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

func append(l string) {
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		panic(err)
	}

	defer f.Close()

	if _, err = f.WriteString(l + "\n"); err != nil {
		panic(err)
	}

	f.Close()
}

func main() {
	append(fmt.Sprintf("%v", os.Args))

	// Needed?
	flag.Parse()

	e := parseConfig()
	if e != nil {
		retErr(fv.StatusFailure, fmt.Sprintf("Failed to parse config file: %v", e))
	}

	if len(os.Args) <= 1 {
		retErr(fv.StatusFailure, "Not Enough Arguments")
	}

	grpcClient, e := dial(config.InfraSocket)
	if e != nil {
		retErr(fv.StatusFailure, "Failed to connect to Infranetes")
	}

	infraClient = common.NewMountsClient(grpcClient)

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
		if len(os.Args) != 4 {
			retErr(fv.StatusFailure, "mount needs 3 arguments")
		}
		do_mount(os.Args[2], os.Args[3])
	case "unmount":
		if len(os.Args) != 3 {
			retErr(fv.StatusFailure, "unmount needs 1 arguments")
		}

		do_umount(os.Args[2])
	case "getvolumename":
		if len(os.Args) != 3 {
			retErr(fv.StatusFailure, "getvolumename needs 1 arguments")
		}
		do_volume(os.Args[2])
	default:
		retErr(fv.StatusNotSupported, fmt.Sprintf("Unknown Command: %v", os.Args[1]))
	}
}
