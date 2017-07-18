package common

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/golang/glog"

	"github.com/kr/pty"
	"k8s.io/client-go/tools/remotecommand"
	kubecontainer "k8s.io/kubernetes/pkg/kubelet/container"
	utilexec "k8s.io/kubernetes/pkg/util/exec"
	"k8s.io/kubernetes/pkg/util/term"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1/runtime"
)

// Essentially used the same concept of version in dockertools/docker_manager.go
func PortForward(podSandboxID string, port int32, stream io.ReadWriteCloser) error {
	if port < 0 || port > math.MaxUint16 {
		return fmt.Errorf("invalid port %d", port)
	}

	socatPath, lookupErr := exec.LookPath("socat")
	if lookupErr != nil {
		return fmt.Errorf("unable to do port forwarding: socat not found.")
	}

	args := []string{"-", fmt.Sprintf("TCP4:localhost:%d", port)}

	commandString := fmt.Sprintf("%s %s", socatPath, strings.Join(args, " "))
	glog.V(4).Infof("executing port forwarding command: %s", commandString)

	command := exec.Command(socatPath, args...)
	command.Stdout = stream

	stderr := new(bytes.Buffer)
	command.Stderr = stderr

	inPipe, err := command.StdinPipe()
	if err != nil {
		return fmt.Errorf("unable to do port forwarding: error creating stdin pipe: %v", err)
	}
	go func() {
		io.Copy(inPipe, stream)
		inPipe.Close()
	}()

	if err := command.Run(); err != nil {
		return fmt.Errorf("%v: %s", err, stderr.String())
	}

	return nil
}

func Exec(cmd []string, stdin io.Reader, stdout, stderr io.WriteCloser, tty bool, resize <-chan remotecommand.TerminalSize) error {
	command := exec.Command(cmd[0], cmd[1:]...)
	var err error
	if tty {
		p, perr := pty.Start(command)
		if perr != nil {
			return perr
		}
		defer p.Close()

		// make sure to close the stdout stream
		defer stdout.Close()

		kubecontainer.HandleResizing(resize, func(size remotecommand.TerminalSize) {
			term.SetSize(p.Fd(), size)
		})

		if stdin != nil {
			go io.Copy(p, stdin)
		}

		if stdout != nil {
			go io.Copy(stdout, p)
		}

		err = command.Wait()
	} else {
		if stdin != nil {
			// Use an os.Pipe here as it returns true *os.File objects.
			// This way, if you run 'kubectl exec <pod> -i bash' (no tty) and type 'exit',
			// the call below to command.Run() can unblock because its Stdin is the read half
			// of the pipe.
			r, w, err := os.Pipe()
			if err != nil {
				return err
			}
			go io.Copy(w, stdin)

			command.Stdin = r
		}
		if stdout != nil {
			command.Stdout = stdout
		}
		if stderr != nil {
			command.Stderr = stderr
		}

		err = command.Run()
	}

	if exitErr, ok := err.(*exec.ExitError); ok {
		return &utilexec.ExitErrorWrapper{ExitError: exitErr}
	}

	return err
}

func ExecSync(req *kubeapi.ExecSyncRequest) (*kubeapi.ExecSyncResponse, error) {
	stdoutBuffer := new(bytes.Buffer)
	stderrBuffer := new(bytes.Buffer)

	command := exec.Command(req.Cmd[0], req.Cmd[1:]...)
	command.Stdout = stdoutBuffer
	command.Stderr = stderrBuffer

	err := command.Run()

	exit := int32(0)

	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			return nil, fmt.Errorf("ExecSync: failed to run: %v", err)
		}
		if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			exit = int32(ws.ExitStatus())
		}
	}

	resp := &kubeapi.ExecSyncResponse{
		Stderr:   stderrBuffer.Bytes(),
		Stdout:   stdoutBuffer.Bytes(),
		ExitCode: exit,
	}

	return resp, nil
}
