package vmserver

import (
	"io/ioutil"
	"os"
	"time"

	"github.com/golang/glog"
	"github.com/sjpotter/infranetes/pkg/common"
	"golang.org/x/net/context"

	"k8s.io/apimachinery/pkg/runtime"
	kubeproxy "k8s.io/kubernetes/cmd/kube-proxy/app"
	"k8s.io/kubernetes/pkg/apis/componentconfig"
	"k8s.io/kubernetes/pkg/apis/componentconfig/v1alpha1"
	"k8s.io/kubernetes/pkg/kubelet/qos"
)

var (
	MasqueradeBit  = int32(14)
	DropBit        = int32(15)
	OOMScoreAdj    = int32(qos.KubeProxyOOMScoreAdj)
	kubeconfigPath = "/var/lib/kube-proxy/"
	kubeconfig     = kubeconfigPath + "kubeconfig"
)

func (m *VMserver) StartProxy(ctx context.Context, req *common.StartProxyRequest) (*common.StartProxyResponse, error) {
	createTables()

	config := &componentconfig.KubeProxyConfiguration{}

	if err := os.MkdirAll(kubeconfigPath, 0700); err != nil {
		glog.Infof("MkdirAll failed: %v", err)
		return nil, err
	}

	err := ioutil.WriteFile(kubeconfig, req.Kubeconfig, 0600)

	// master details
	master := "https://" + req.Ip
	config.ClusterCIDR = req.ClusterCidr
	config.ClientConnection.KubeConfigFile = kubeconfig
	config.HealthzBindAddress = "0.0.0.0:10256"

	syncP, _ := time.ParseDuration("1m")
	config.IPTables.SyncPeriod.Duration = syncP
	//minSyncP, _ := time.ParseDuration("10s")
	//config.IPTables.MinSyncPeriod.Duration = minSyncP

	scheme := runtime.NewScheme()
	if err := componentconfig.AddToScheme(scheme); err != nil {
		return nil, err
	}
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		return nil, err
	}

	config.Mode = componentconfig.ProxyModeIPTables

	// defaults
	config.OOMScoreAdj = &OOMScoreAdj
	config.IPTables.MasqueradeBit = &MasqueradeBit
	//config.IPTables.MasqueradeAll = true
	config.BindAddress = *m.podIp

	server, err := kubeproxy.NewProxyServer(config, false, scheme, master)
	if err != nil {
		glog.Infof("NewProxyServer failed: %v", err)
		return nil, err
	}

	go func() {
		err := server.Run()
		glog.Infof("server.Run failed: %v", err)
	}()

	return &common.StartProxyResponse{}, nil
}
