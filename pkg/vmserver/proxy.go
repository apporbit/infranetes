package vmserver

import (
	"io/ioutil"
	"os"

	"github.com/sjpotter/infranetes/pkg/common"
	"golang.org/x/net/context"

	"github.com/golang/glog"
	kubeproxy "k8s.io/kubernetes/cmd/kube-proxy/app"
	"k8s.io/kubernetes/cmd/kube-proxy/app/options"
	"k8s.io/kubernetes/pkg/apis/componentconfig"
	"k8s.io/kubernetes/pkg/kubelet/qos"
)

var (
	MasqueradeBit  = int32(14)
	OOMScoreAdj    = int32(qos.KubeProxyOOMScoreAdj)
	kubeconfigPath = "/var/lib/kube-proxy/"
	kubeconfig     = kubeconfigPath + "kubeconfig"
)

func (m *VMserver) StartProxy(ctx context.Context, req *common.StartProxyRequest) (*common.StartProxyResponse, error) {
	config := options.NewProxyConfig()

	if err := os.MkdirAll(kubeconfigPath, 0700); err != nil {
		glog.Infof("MkdirAll failed: %v", err)
		return nil, err
	}

	err := ioutil.WriteFile(kubeconfig, req.Kubeconfig, 0600)

	// master details
	config.Master = "https://" + req.Ip
	config.ClusterCIDR = req.ClusterCidr
	config.Kubeconfig = kubeconfig

	config.Mode = componentconfig.ProxyModeIPTables

	// defaults
	config.OOMScoreAdj = &OOMScoreAdj
	config.IPTablesMasqueradeBit = &MasqueradeBit

	server, err := kubeproxy.NewProxyServerDefault(config)
	if err != nil {
		glog.Infof("NewProxyServerDefault failed: %v", err)
		return nil, err
	}

	go func() {
		err := server.Run()
		glog.Infof("server.Run failed: %v", err)
	}()

	return &common.StartProxyResponse{}, nil
}
