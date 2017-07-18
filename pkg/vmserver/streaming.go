package vmserver

import (
	"golang.org/x/net/context"

	"github.com/golang/glog"
	"k8s.io/kubernetes/pkg/kubelet/server/streaming"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1/runtime"
)

func (m *VMserver) startStreamingServer() error {
	runtime := m.contProvider.GetStreamingRuntime()

	if runtime == nil {
		glog.Infof("startStreamingServer: runtime is nil, no streamingServer")
		m.streamingServer = nil
		return nil
	}

	if m.podIp == nil {
		glog.Infof("startStreamingServer: podIp is nil, no streamingServer")
		m.streamingServer = nil
		return nil
	}

	addr := *m.podIp + ":12345"

	//TODO(sjpotter): Figure out how to work with TLS?
	config := streaming.Config{
		Addr: addr,
		StreamCreationTimeout:           streaming.DefaultConfig.StreamCreationTimeout,
		StreamIdleTimeout:               streaming.DefaultConfig.StreamIdleTimeout,
		SupportedRemoteCommandProtocols: streaming.DefaultConfig.SupportedRemoteCommandProtocols,
		SupportedPortForwardProtocols:   streaming.DefaultConfig.SupportedPortForwardProtocols,
	}

	streamingServer, err := streaming.NewServer(config, runtime)
	if err != nil {
		glog.Infof("StartStreamingServer: NewServer failed: %v", err)
		m.streamingServer = nil
		return nil
	}

	m.streamingServer = streamingServer

	go func() {
		streamingServer.Start(true)
	}()

	return err
}

func (m *VMserver) Exec(ctx context.Context, req *kubeapi.ExecRequest) (*kubeapi.ExecResponse, error) {
	if m.streamingServer == nil {
		return nil, streaming.ErrorStreamingDisabled("exec")
	}

	return m.streamingServer.GetExec(req)
}

func (m *VMserver) Attach(ctx context.Context, req *kubeapi.AttachRequest) (*kubeapi.AttachResponse, error) {
	if m.streamingServer == nil {
		return nil, streaming.ErrorStreamingDisabled("attach")
	}

	return m.streamingServer.GetAttach(req)
}

// In traditional kubernetes land this has to nsenter the network namespace of the pod, in Infranetes, there's only one namespace
func (m *VMserver) PortForward(ctx context.Context, req *kubeapi.PortForwardRequest) (*kubeapi.PortForwardResponse, error) {
	if m.streamingServer == nil {
		return nil, streaming.ErrorStreamingDisabled("port forward")
	}

	return m.streamingServer.GetPortForward(req)
}
