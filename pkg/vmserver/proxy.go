package vmserver

import (
	"github.com/sjpotter/infranetes/pkg/common"
	"golang.org/x/net/context"

	kubeproxy "k8s.io/kubernetes/cmd/kube-proxy/app"
	"k8s.io/kubernetes/cmd/kube-proxy/app/options"
	"k8s.io/kubernetes/pkg/apis/componentconfig"
	"k8s.io/kubernetes/pkg/kubelet/qos"
)

var (
	MasqueradeBit = int32(14)
	OOMScoreAdj   = int32(qos.KubeProxyOOMScoreAdj)
)

func (m *VMserver) StartProxy(ctx context.Context, ip *common.IPAddress) (*common.StartProxyResponse, error) {
	config := options.NewProxyConfig()

	// master details
	config.Master = "http://" + ip.Ip + ":" + "8080"

	config.Mode = componentconfig.ProxyModeIPTables

	// defaults
	config.OOMScoreAdj = &OOMScoreAdj
	config.IPTablesMasqueradeBit = &MasqueradeBit

	server, err := kubeproxy.NewProxyServerDefault(config)
	if err != nil {
		return nil, err
	}

	go server.Run()

	return &common.StartProxyResponse{}, nil
}
