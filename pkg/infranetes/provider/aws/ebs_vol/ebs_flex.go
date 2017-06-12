package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"time"

	//	"github.com/golang/glog"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/apporbit/infranetes/pkg/common"
	fv "k8s.io/kubernetes/pkg/volume/flexvolume"
	"net/http"
)

// FIXME: make these configurable from a configuration file
var (
	// Iso is location for the fake mount to loopback from, can switch this to a smaller squashfs file, unsure makes a huge difference
	Iso string = "/tmp/test.iso"

	// InfraSocket is the location for the infrantes socket
	InfraSocket string = "/tmp/infra"

	kubelexPrefix = "/var/lib/kubelet/"

	instanceId = ""

	formatDevice = "/dev/xvdz"

	region = "us-west-2"

	availZone = "us-west-2a"
)

var (
	infraClient common.MountsClient
	ec2Client   *ec2.EC2
)

// FIXME: can't handle attach like this, as doesn't return "device"
func retOk() {
	status := fv.DriverStatus{Status: fv.StatusSuccess}
	b, _ := json.Marshal(status)

	fmt.Printf("%v", string(b))

	//	glog.Flush()
	os.Exit(0)
}

func retErr(s, m string) {
	status := fv.DriverStatus{Status: s, Message: m}
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
		if !err {
			retErr(fv.StatusFailure, fmt.Sprintf("do_mount: faile to provision volume: %v", err))
		}

		vol = *newVol
		opts["format"] = "true"
	}

	if f, ok := opts["format"]; ok {
		format, err := strconv.ParseBool(f)
		if err == nil && format {
			do_format(vol, fsType)
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

	cmd := exec.Command("mount", "-o", "loop", Iso, mntpnt)
	output, e := cmd.CombinedOutput()
	if e != nil {
		infraClient.DelMount(context.Background(), &common.DelMountRequest{MountPoint: mntpnt})
		msg := fmt.Sprintf("do_mount: mntpnt: %v, flexOpts: %v: output %v", mntpnt, flexOpts, string(output))
		retErr(fv.StatusFailure, msg)
	}

	retOk()
}

func provision(size int64) (*string, error) {
	if ec2Client == nil {
		initEC2()
	}

	req := &ec2.CreateVolumeInput{
		Size:             &size,
		AvailabilityZone: &availZone,
	}

	resp, err := ec2Client.CreateVolume(req)
	if err != nil {
		return nil, fmt.Errorf("provision failed: %v", err)
	}

	return resp.VolumeId, nil
}

func do_format(vol, fsType string) error {
	if ec2Client == nil {
		initEC2()
	}

	if err := do_attach(vol); err != nil {
		return fmt.Errorf("do_format: attach failed: %v", err)
	}

	switch fsType {
	case "ext4":
		cmd := exec.Command("mkfs.ext4", formatDevice)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("do_format: mkfs.ext4 failed: %v", string(output))
		}
	default:
		return fmt.Errorf("do_format: unknown fstype: %v", fsType)
	}

	if err := do_detach(vol); err != nil {
		return fmt.Errorf("do_format: detach failed: %v", err)
	}

	return nil
}

func do_attach(vol string) error {
	req := &ec2.AttachVolumeInput{
		VolumeId:   &vol,
		Device:     &formatDevice,
		InstanceId: &instanceId,
	}

	_, err := ec2Client.AttachVolume(req)
	if err != nil {
		return err
	}

	if err := wait_attach(vol); err != nil {
		return fmt.Errorf("do_attach: attach never being active: %v", err)
	}

	return nil
}

func do_detach(vol string) error {
	req := &ec2.DetachVolumeInput{
		VolumeId: &vol,
	}

	_, err := ec2Client.DetachVolume(req)
	if err != nil {
		return err
	}

	if err := wait_detach(vol); err != nil {
		return fmt.Errorf("do_detach: detach never succeeded: %v", err)
	}

	return nil
}

func wait_detach(vol string) error {
	req := &ec2.DescribeVolumesInput{
		VolumeIds: []*string{&vol},
	}

	for i := 1; i <= 5; i++ {
		resp, err := ec2Client.DescribeVolumes(req)
		if err != nil {
			return err
		}

		if len(resp.Volumes) != 1 {
			return fmt.Errorf("wait_detach: describe didn't return any volumes")
		}

		if resp.Volumes[0].State == "available" {
			return nil
		}

		time.Sleep(time.Duration(i) * time.Second)
	}

	return fmt.Errorf("timed out waiting for volume detachment")
}

func wait_attach(vol string) error {
	req := &ec2.DescribeVolumesInput{
		VolumeIds: []*string{&vol},
	}

	for i := 1; i <= 5; i++ {
		resp, err := ec2Client.DescribeVolumes(req)
		if err != nil {
			return err
		}

		if len(resp.Volumes) != 1 {
			return fmt.Errorf("wait_attach: describe didn't return any volumes")
		}
		if len(resp.Volumes[0].Attachments) == 1 {
			if "attached" == *resp.Volumes[0].Attachments[0].State {
				return nil
			}
		}

		time.Sleep(time.Duration(i) * time.Second)
	}

	return fmt.Errorf("timed out waiting for volume attachment")
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

func initEC2() {
	creds := credentials.NewChainCredentials(
		[]credentials.Provider{
			&credentials.EnvProvider{},               // check environment
			&credentials.SharedCredentialsProvider{}, // check home dir
		},
	)

	if region == "" { // user didn't set region
		region = os.Getenv("AWS_DEFAULT_REGION") // aws cli checks this
		if region == "" {
			region = os.Getenv("AWS_REGION") // aws sdk checks this
		}
	}

	ec2Client = ec2.New(session.New(&aws.Config{
		Credentials: creds,
		Region:      &region,
		//CredentialsChainVerboseErrors: aws.Bool(true),
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}))

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
