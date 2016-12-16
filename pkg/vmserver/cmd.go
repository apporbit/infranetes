package vmserver

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/net/context"

	"github.com/golang/glog"
	"github.com/sjpotter/infranetes/pkg/common"
	"syscall"
)

func (m *VMserver) RunCmd(ctx context.Context, req *common.RunCmdRequest) (*common.RunCmdResponse, error) {
	cmd := exec.Command(req.Cmd, req.Args...)
	err := cmd.Run()

	return &common.RunCmdResponse{}, err
}

func (m *VMserver) SetPodIP(ctx context.Context, req *common.SetIPRequest) (*common.SetIPResponse, error) {
	val := net.ParseIP(req.Ip)
	if val == nil {
		return nil, fmt.Errorf("SetPodIP: %v is an invalid ip address", req.Ip)
	}

	args := []string{"eth0:0", req.Ip, "netmask", "255.255.255.255"}

	cmd := exec.Command("ifconfig", args...)
	err := cmd.Run()

	if err == nil {
		m.podIp = &req.Ip
	}

	err = m.startStreamingServer()
	if err != nil {
		glog.Warning("SetPodIP: couldn't start streaming server for exec/attach")
	}

	return &common.SetIPResponse{}, err
}

func (m *VMserver) GetPodIP(ctx context.Context, req *common.GetIPRequest) (*common.GetIPResponse, error) {
	if m.podIp == nil {
		return nil, fmt.Errorf("GetPodIP: podIp wasn't set")
	}

	return &common.GetIPResponse{Ip: *m.podIp}, nil
}

func (m *VMserver) SetSandboxConfig(ctx context.Context, req *common.SetSandboxConfigRequest) (*common.SetSandboxConfigResponse, error) {
	m.config = req.Config

	return &common.SetSandboxConfigResponse{}, nil
}

func (m *VMserver) GetSandboxConfig(ctx context.Context, req *common.GetSandboxConfigRequest) (*common.GetSandboxConfigResponse, error) {
	if m.config == nil {
		return nil, fmt.Errorf("GetSandboxConfig: sandbox config wasn't set")
	}

	return &common.GetSandboxConfigResponse{Config: m.config}, nil
}

func (m *VMserver) CopyFile(ctx context.Context, req *common.CopyFileRequest) (*common.CopyFileResponse, error) {
	_, err := os.Stat(req.File)
	if err == nil {
		// File Exists
		return &common.CopyFileResponse{}, nil
	}

	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("CopyFile: stat failed: %v", err)
	}

	dir := filepath.Dir(req.File)

	err = os.MkdirAll(dir, 0755)
	if err != nil {
		return nil, fmt.Errorf("CopyFile: MkdirAll failed: %v", err)
	}
	err = ioutil.WriteFile(req.File, req.FileData, 0644)
	if err != nil {
		return nil, fmt.Errorf("CopyFile: WriteFile failed: %v", err)
	}

	return &common.CopyFileResponse{}, nil
}

func (m *VMserver) MountFs(ctx context.Context, req *common.MountFsRequest) (*common.MountFsResponse, error) {
	glog.Infof("MountFS: Attempint to mount %v on %v with readonly = %v", req.Source, req.Target, req.ReadOnly)
	mountCmd := "/bin/mount"
	rw := "rw"
	if req.ReadOnly {
		rw = "ro"
	}
	mountArgs := []string{"-t", req.Fstype, "-o", rw, req.Source, req.Target}

	err := os.MkdirAll(req.Target, 0755)
	if err != nil {
		return nil, fmt.Errorf("MountFs: MkdirAll failed: %v", err)
	}

	command := exec.Command(mountCmd, mountArgs...)
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("MountFs: mount failed:\n output = %v", output)
	}

	return &common.MountFsResponse{}, err
}

func (m *VMserver) SetHostname(ctx context.Context, req *common.SetHostnameRequest) (*common.SetHostnameResponse, error) {
	bytes := []byte(req.Hostname)

	err := syscall.Sethostname(bytes)

	return &common.SetHostnameResponse{}, err
}
