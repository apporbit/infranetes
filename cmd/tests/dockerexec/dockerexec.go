package main

import (
	"bytes"
	"fmt"
	"os"
	"time"

	"github.com/docker/docker/pkg/ioutils"
	dockerclient "github.com/docker/engine-api/client"
	dockertypes "github.com/docker/engine-api/types"
	"k8s.io/kubernetes/pkg/kubelet/dockershim"
	"k8s.io/kubernetes/pkg/kubelet/dockershim/libdocker"
)

func main() {
	args := os.Args

	var cmd []string
	var contId string
	for i, arg := range args {
		switch i {
		case 0:
			break
		case 1:
			contId = arg
		default:
			cmd = append(cmd, arg)
		}
	}

	client, err := dockerclient.NewClient(dockerclient.DefaultDockerHost, "", nil, nil)
	if err != nil {
		fmt.Printf("docker New Client failed: %v", err)
	}

	dc := libdocker.KubeWrapDockerclient(client)

	exec := dockershim.NativeExecHandler{}

	cont, err := checkContainerStatus(dc, contId)
	if err != nil {
		fmt.Printf("checkContainerStatus failed: %v", err)
		return
	}

	stdout := bytes.NewBuffer([]byte{})
	stderr := bytes.NewBuffer([]byte{})

	stdoutw := ioutils.NewWriteCloserWrapper(stdout, func() error { return nil })
	stderrw := ioutils.NewWriteCloserWrapper(stderr, func() error { return nil })

	err = exec.ExecInContainer(dc, cont, cmd, nil, stdoutw, stderrw, false, nil, 10*time.Second)
	if err != nil {
		fmt.Printf("ExecInContainer failed: err = %v", err)
		return
	}
	fmt.Printf("stdout = %v", string(stdout.Bytes()))
	fmt.Printf("stderr = %v", string(stderr.Bytes()))
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
