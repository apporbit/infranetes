package vmserver

import (
	"golang.org/x/net/context"

	"fmt"
	"github.com/sjpotter/infranetes/pkg/common"
	"os/exec"
)

func (m *VMserver) RunCmd(ctx context.Context, req *common.RunCmdRequest) (*common.RunCmdResponse, error) {
	cmd := exec.Command(req.Cmd, req.Args...)
	err := cmd.Run()

	return &common.RunCmdResponse{}, err
}

func (m *VMserver) SetPodIP(ctx context.Context, req *common.SetIPRequest) (*common.SetIPResponse, error) {
	args := []string{"eth0:0", req.Ip, "netmask", "255.255.255.255"}

	cmd := exec.Command("ifconfig", args...)
	err := cmd.Run()

	if err == nil {
		m.podIp = &req.Ip
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
