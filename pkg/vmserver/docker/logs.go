package docker

import (
	"fmt"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/hpcloud/tail"

	"github.com/sjpotter/infranetes/pkg/common"
)

func (d *dockerProvider) Logs(req *common.LogsRequest, stream common.VMServer_LogsServer) error {
	resp, err := d.client.ContainerInspect(context.Background(), req.ContainerID)
	if err != nil {
		return fmt.Errorf("Logs: docker container inspect failed")
	}

	t, err := tail.TailFile(resp.LogPath, tail.Config{Follow: true})
	if err != nil {
		return fmt.Errorf("Logs: tail failed")
	}

	d.setTail(req.ContainerID, t)

	for line := range t.Lines {
		glog.Infof("Log: line = %v", line.Text)
		err = stream.Send(&common.LogLine{LogLine: line.Text})
		if err != nil {
			glog.Warningf("Log: Client side Ended early?: %v", err)
			break
		}
	}

	return err
}
