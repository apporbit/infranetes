package vmserver

import (
	"golang.org/x/net/context"

	"github.com/sjpotter/infranetes/pkg/common"
	"os/exec"
)

func (m *VMserver) RunCmd(ctx context.Context, req *common.RunCmdRequest) (*common.RunCmdResponse, error) {
	cmd := exec.Command(req.Cmd, req.Args...)
	err := cmd.Run()

	return &common.RunCmdResponse{}, err
}
