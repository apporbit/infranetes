package fake

import (
	"fmt"
	"io"

	"k8s.io/kubernetes/pkg/util/term"

	icommon "github.com/sjpotter/infranetes/pkg/common"
	"github.com/sjpotter/infranetes/pkg/vmserver/common"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
	"k8s.io/kubernetes/pkg/kubelet/server/streaming"
)

type podExecProvider struct{}

func (p *podExecProvider) ExecSync(req *kubeapi.ExecSyncRequest) (*kubeapi.ExecSyncResponse, error) {
	return common.ExecSync(req)
}

func (f *podExecProvider) GetStreamingRuntime() streaming.Runtime {
	return nil
}

func (d *podExecProvider) Logs(req *icommon.LogsRequest, stream icommon.VMServer_LogsServer) error {
	return fmt.Errorf("Logging not currently support in vm mode yet")
}

type streamingRuntime struct{}

func (r *streamingRuntime) Exec(containerID string, cmd []string, in io.Reader, out, err io.WriteCloser, tty bool, resize <-chan term.Size) error {
	return common.Exec(cmd, in, out, err, tty, resize)
}

func (r *streamingRuntime) Attach(containerID string, in io.Reader, out, errw io.WriteCloser, tty bool, resize <-chan term.Size) error {
	return fmt.Errorf("Attach currently unsupported for VMs")
}

func (r *streamingRuntime) PortForward(podSandboxID string, port int32, stream io.ReadWriteCloser) error {
	return common.PortForward(podSandboxID, port, stream)
}
