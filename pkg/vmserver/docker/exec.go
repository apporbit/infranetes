package docker

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"strings"
	"time"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	dockerstdcopy "github.com/docker/docker/pkg/stdcopy"
	dockerapi "github.com/docker/engine-api/client"
	dockertypes "github.com/docker/engine-api/types"

	"k8s.io/client-go/tools/remotecommand"
	kubecontainer "k8s.io/kubernetes/pkg/kubelet/container"
	"k8s.io/kubernetes/pkg/kubelet/util/ioutils"
	utilexec "k8s.io/kubernetes/pkg/util/exec"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1"
)

// operationTimeout is the error returned when the docker operations are timeout.
type operationTimeout struct {
	err error
}

func (e operationTimeout) Error() string {
	return fmt.Sprintf("operation timeout: %v", e.err)
}

type dockerExitError struct {
	Inspect *dockertypes.ContainerExecInspect
}

func (d *dockerExitError) String() string {
	return d.Error()
}

func (d *dockerExitError) Error() string {
	return fmt.Sprintf("Error executing in Docker Container: %d", d.Inspect.ExitCode)
}

func (d *dockerExitError) Exited() bool {
	return !d.Inspect.Running
}

func (d *dockerExitError) ExitStatus() int {
	return d.Inspect.ExitCode
}

type containerNotFoundError struct {
	ID string
}

func (e containerNotFoundError) Error() string {
	return fmt.Sprintf("no such container: %q", e.ID)
}

// contextError checks the context, and returns error if the context is timeout.
func contextError(ctx context.Context) error {
	if ctx.Err() == context.DeadlineExceeded {
		return operationTimeout{err: ctx.Err()}
	}
	return ctx.Err()
}

func (p *dockerProvider) CreateExec(id string, opts dockertypes.ExecConfig) (*dockertypes.ContainerExecCreateResponse, error) {
	ctx, cancel := getTimeoutContext()
	defer cancel()
	resp, err := p.client.ContainerExecCreate(ctx, id, opts)
	if ctxErr := contextError(ctx); ctxErr != nil {
		return nil, ctxErr
	}
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (p *dockerProvider) StartExec(startExec string, opts dockertypes.ExecStartCheck, sopts StreamOptions) error {
	ctx, cancel := getCancelableContext()
	defer cancel()
	if opts.Detach {
		err := p.client.ContainerExecStart(ctx, startExec, opts)
		if ctxErr := contextError(ctx); ctxErr != nil {
			return ctxErr
		}
		return err
	}
	resp, err := p.client.ContainerExecAttach(ctx, startExec, dockertypes.ExecConfig{
		Detach: opts.Detach,
		Tty:    opts.Tty,
	})
	if ctxErr := contextError(ctx); ctxErr != nil {
		return ctxErr
	}
	if err != nil {
		return err
	}
	defer resp.Close()
	return p.holdHijackedConnection(sopts.RawTerminal || opts.Tty, sopts.InputStream, sopts.OutputStream, sopts.ErrorStream, resp)
}

func (p *dockerProvider) InspectExec(id string) (*dockertypes.ContainerExecInspect, error) {
	ctx, cancel := getTimeoutContext()
	defer cancel()
	resp, err := p.client.ContainerExecInspect(ctx, id)
	if ctxErr := contextError(ctx); ctxErr != nil {
		return nil, ctxErr
	}
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (p *dockerProvider) holdHijackedConnection(tty bool, inputStream io.Reader, outputStream, errorStream io.Writer, resp dockertypes.HijackedResponse) error {
	receiveStdout := make(chan error)
	if outputStream != nil || errorStream != nil {
		go func() {
			receiveStdout <- redirectResponseToOutputStream(tty, outputStream, errorStream, resp.Reader)
		}()
	}

	stdinDone := make(chan struct{})
	go func() {
		if inputStream != nil {
			io.Copy(resp.Conn, inputStream)
		}
		resp.CloseWrite()
		close(stdinDone)
	}()

	select {
	case err := <-receiveStdout:
		return err
	case <-stdinDone:
		if outputStream != nil || errorStream != nil {
			return <-receiveStdout
		}
	}
	return nil
}

func redirectResponseToOutputStream(tty bool, outputStream, errorStream io.Writer, resp io.Reader) error {
	if outputStream == nil {
		outputStream = ioutil.Discard
	}
	if errorStream == nil {
		errorStream = ioutil.Discard
	}
	var err error
	if tty {
		_, err = io.Copy(outputStream, resp)
	} else {
		_, err = dockerstdcopy.StdCopy(outputStream, errorStream, resp)
	}
	return err
}

func (p *dockerProvider) ResizeExecTTY(id string, height, width int) error {
	ctx, cancel := getCancelableContext()
	defer cancel()
	return p.client.ContainerExecResize(ctx, id, dockertypes.ResizeOptions{
		Height: height,
		Width:  width,
	})
}

type StreamOptions struct {
	RawTerminal  bool
	InputStream  io.Reader
	OutputStream io.Writer
	ErrorStream  io.Writer
}

func (p *dockerProvider) ExecInContainer(container *dockertypes.ContainerJSON, cmd []string, stdin io.Reader, stdout, stderr io.WriteCloser, tty bool, resize <-chan remotecommand.TerminalSize, timeout time.Duration) error {
	createOpts := dockertypes.ExecConfig{
		Cmd:          cmd,
		AttachStdin:  stdin != nil,
		AttachStdout: stdout != nil,
		AttachStderr: stderr != nil,
		Tty:          tty,
	}
	execObj, err := p.CreateExec(container.ID, createOpts)
	if err != nil {
		return fmt.Errorf("failed to exec in container - Exec setup failed - %v", err)
	}

	// Have to start this before the call to client.StartExec because client.StartExec is a blocking
	// call :-( Otherwise, resize events don't get processed and the terminal never resizes.
	kubecontainer.HandleResizing(resize, func(size remotecommand.TerminalSize) {
		p.ResizeExecTTY(execObj.ID, int(size.Height), int(size.Width))
	})

	startOpts := dockertypes.ExecStartCheck{Detach: false, Tty: tty}
	streamOpts := StreamOptions{
		InputStream:  stdin,
		OutputStream: stdout,
		ErrorStream:  stderr,
		RawTerminal:  tty,
	}
	err = p.StartExec(execObj.ID, startOpts, streamOpts)
	if err != nil {
		return err
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	count := 0
	for {
		inspect, err2 := p.InspectExec(execObj.ID)
		if err2 != nil {
			return err2
		}
		if !inspect.Running {
			if inspect.ExitCode != 0 {
				err = &dockerExitError{inspect}
			}
			break
		}
		count++
		if count == 5 {
			glog.Errorf("Exec session %s in container %s terminated but process still running!", execObj.ID, container.ID)
			break
		}

		<-ticker.C
	}

	return err
}

func (p *dockerProvider) ExecSync(req *kubeapi.ExecSyncRequest) (*kubeapi.ExecSyncResponse, error) {
	var stdoutBuffer, stderrBuffer bytes.Buffer

	splits := strings.Split(req.GetContainerId(), ":")
	contId := splits[1]

	container, err := p.checkContainerStatus(contId)
	if err != nil {
		return nil, err
	}

	err = p.ExecInContainer(container, req.GetCmd(),
		nil, //stdin
		ioutils.WriteCloserWrapper(&stdoutBuffer),
		ioutils.WriteCloserWrapper(&stderrBuffer),
		false, // tty
		nil,   // resize
		time.Duration(req.GetTimeout())*time.Second)

	var exitCode int32
	if err != nil {
		exitError, ok := err.(utilexec.ExitError)
		if !ok {
			return nil, err
		}
		exitCode = int32(exitError.ExitStatus())
	}

	ret := &kubeapi.ExecSyncResponse{
		Stdout:   stdoutBuffer.Bytes(),
		Stderr:   stderrBuffer.Bytes(),
		ExitCode: exitCode,
	}

	return ret, nil
}

func (p *dockerProvider) checkContainerStatus(id string) (*dockertypes.ContainerJSON, error) {
	ctx, cancel := getTimeoutContext()
	defer cancel()
	container, err := p.client.ContainerInspect(ctx, id)
	if ctxErr := contextError(ctx); ctxErr != nil {
		return nil, ctxErr
	}
	if err != nil {
		if dockerapi.IsErrContainerNotFound(err) {
			return nil, containerNotFoundError{ID: id}
		}
		return nil, err
	}

	if !container.State.Running {
		return nil, fmt.Errorf("container not running (%s)", container.ID)
	}

	return &container, nil
}
