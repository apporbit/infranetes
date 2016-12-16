package docker

import (
	"fmt"
	"io"
	"time"

	"github.com/golang/glog"

	dockertypes "github.com/docker/engine-api/types"
	"k8s.io/kubernetes/pkg/kubelet/dockertools"
	"k8s.io/kubernetes/pkg/util/term"

	icommon "github.com/sjpotter/infranetes/pkg/common"
	"github.com/sjpotter/infranetes/pkg/vmserver/common"

	"k8s.io/kubernetes/pkg/kubelet/server/streaming"
)

type streamingRuntime struct {
	client      dockertools.DockerInterface
	execHandler dockertools.ExecHandler
}

var _ streaming.Runtime = &streamingRuntime{}

func (d *dockerProvider) GetStreamingRuntime() streaming.Runtime {
	return d.streamingRuntime
}

func (r *streamingRuntime) Exec(containerID string, cmd []string, in io.Reader, out, err io.WriteCloser, tty bool, resize <-chan term.Size) error {
	return r.exec(containerID, cmd, in, out, err, tty, resize, 0)
}

// Internal version of Exec adds a timeout.
func (r *streamingRuntime) exec(containerID string, cmd []string, in io.Reader, out, errw io.WriteCloser, tty bool, resize <-chan term.Size, timeout time.Duration) error {
	glog.Infof("Exec: containerID = %v, cmd = %+v", containerID, cmd)
	_, cont, err := icommon.ParseContainer(containerID)
	if err != nil {
		return fmt.Errorf("exec: err = %v", err)
	}

	container, err := checkContainerStatus(r.client, cont)
	if err != nil {
		glog.Infof("Exec: checkContainerStatus failed: %v", err)
		return err
	}

	err = r.execHandler.ExecInContainer(r.client, container, cmd, in, out, errw, tty, resize, timeout)

	glog.Infof("Exec (exit): err = %v", err)

	return err
}

func (r *streamingRuntime) Attach(containerID string, in io.Reader, out, errw io.WriteCloser, tty bool, resize <-chan term.Size) error {
	glog.Infof("Attach: containerID = %v", containerID)
	_, cont, err := icommon.ParseContainer(containerID)
	if err != nil {
		return fmt.Errorf("Attach: err = %v", err)
	}

	_, err = checkContainerStatus(r.client, cont)
	if err != nil {
		glog.Infof("Attach: checkContainerStatus failed: %v", err)
		return err
	}

	err = dockertools.AttachContainer(r.client, cont, in, out, errw, tty, resize)

	glog.Infof("Attach (exit): err = %v", err)

	return err
}

// As just VM based, can call the common version
func (r *streamingRuntime) PortForward(podSandboxID string, port int32, stream io.ReadWriteCloser) error {
	return common.PortForward(podSandboxID, port, stream)
}

func checkContainerStatus(client dockertools.DockerInterface, containerID string) (*dockertypes.ContainerJSON, error) {
	cont, err := client.InspectContainer(containerID)
	if err != nil {
		return nil, err
	}
	if !cont.State.Running {
		return nil, fmt.Errorf("container not running (%s)", cont.ID)
	}
	return cont, nil
}
