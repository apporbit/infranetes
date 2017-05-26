package docker

import (
	"fmt"
	"io"
	"time"

	"github.com/golang/glog"

	dockertypes "github.com/docker/engine-api/types"
	"k8s.io/client-go/tools/remotecommand"
	kubecontainer "k8s.io/kubernetes/pkg/kubelet/container"
	"k8s.io/kubernetes/pkg/kubelet/dockershim"
	"k8s.io/kubernetes/pkg/kubelet/dockershim/libdocker"

	icommon "github.com/sjpotter/infranetes/pkg/common"
	"github.com/sjpotter/infranetes/pkg/vmserver/common"

	"k8s.io/kubernetes/pkg/kubelet/server/streaming"
)

type streamingRuntime struct {
	client      libdocker.Interface
	execHandler dockershim.ExecHandler
}

var _ streaming.Runtime = &streamingRuntime{}

func (d *dockerProvider) GetStreamingRuntime() streaming.Runtime {
	return d.streamingRuntime
}

func (r *streamingRuntime) Exec(containerID string, cmd []string, in io.Reader, out, err io.WriteCloser, tty bool, resize <-chan remotecommand.TerminalSize) error {
	return r.exec(containerID, cmd, in, out, err, tty, resize, 0)
}

// Internal version of Exec adds a timeout.
func (r *streamingRuntime) exec(containerID string, cmd []string, in io.Reader, out, errw io.WriteCloser, tty bool, resize <-chan remotecommand.TerminalSize, timeout time.Duration) error {
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

func (r *streamingRuntime) Attach(containerID string, in io.Reader, out, errw io.WriteCloser, tty bool, resize <-chan remotecommand.TerminalSize) error {
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

	err = attachContainer(r.client, cont, in, out, errw, tty, resize)

	glog.Infof("Attach (exit): err = %v", err)

	return err
}
func attachContainer(client libdocker.Interface, containerID string, stdin io.Reader, stdout, stderr io.WriteCloser, tty bool, resize <-chan remotecommand.TerminalSize) error {
	// Have to start this before the call to client.AttachToContainer because client.AttachToContainer is a blocking
	// call :-( Otherwise, resize events don't get processed and the terminal never resizes.
	kubecontainer.HandleResizing(resize, func(size remotecommand.TerminalSize) {
		client.ResizeContainerTTY(containerID, int(size.Height), int(size.Width))
	})

	// TODO(random-liu): Do we really use the *Logs* field here?
	opts := dockertypes.ContainerAttachOptions{
		Stream: true,
		Stdin:  stdin != nil,
		Stdout: stdout != nil,
		Stderr: stderr != nil,
	}
	sopts := libdocker.StreamOptions{
		InputStream:  stdin,
		OutputStream: stdout,
		ErrorStream:  stderr,
		RawTerminal:  tty,
	}
	return client.AttachToContainer(containerID, opts, sopts)
}

// As just VM based, can call the common version
func (r *streamingRuntime) PortForward(podSandboxID string, port int32, stream io.ReadWriteCloser) error {
	return common.PortForward(podSandboxID, port, stream)
}

func checkContainerStatus(client libdocker.Interface, containerID string) (*dockertypes.ContainerJSON, error) {
	cont, err := client.InspectContainer(containerID)
	if err != nil {
		return nil, err
	}
	if !cont.State.Running {
		return nil, fmt.Errorf("container not running (%s)", cont.ID)
	}
	return cont, nil
}
