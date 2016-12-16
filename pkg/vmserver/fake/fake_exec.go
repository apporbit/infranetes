package fake

import (
	"fmt"

	"k8s.io/kubernetes/pkg/kubelet/server/streaming"

	icommon "github.com/sjpotter/infranetes/pkg/common"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

type fakeExecProvider struct {
}

func (f *fakeExecProvider) ExecSync(req *kubeapi.ExecSyncRequest) (*kubeapi.ExecSyncResponse, error) {
	var code int32
	code = 0
	ret := &kubeapi.ExecSyncResponse{
		ExitCode: &code,
	}

	return ret, nil
}

func (f *fakeExecProvider) GetStreamingRuntime() streaming.Runtime {
	return nil
}

func (d *fakeExecProvider) Logs(req *icommon.LogsRequest, stream icommon.VMServer_LogsServer) error {
	return fmt.Errorf("Logging not supported in fake containers")
}
