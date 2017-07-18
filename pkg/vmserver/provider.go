package vmserver

import (
	"fmt"

	"github.com/golang/glog"

	"k8s.io/kubernetes/pkg/kubelet/server/streaming"

	"github.com/apporbit/infranetes/pkg/common"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1/runtime"
)

type ContainerProvider interface {
	CreateContainer(req *kubeapi.CreateContainerRequest) (*kubeapi.CreateContainerResponse, error)
	StartContainer(req *kubeapi.StartContainerRequest) (*kubeapi.StartContainerResponse, error)
	StopContainer(req *kubeapi.StopContainerRequest) (*kubeapi.StopContainerResponse, error)
	RemoveContainer(req *kubeapi.RemoveContainerRequest) (*kubeapi.RemoveContainerResponse, error)
	ListContainers(req *kubeapi.ListContainersRequest) (*kubeapi.ListContainersResponse, error)
	ContainerStatus(req *kubeapi.ContainerStatusRequest) (*kubeapi.ContainerStatusResponse, error)
	ExecSync(req *kubeapi.ExecSyncRequest) (*kubeapi.ExecSyncResponse, error)
	GetStreamingRuntime() streaming.Runtime
	Logs(req *common.LogsRequest, stream common.VMServer_LogsServer) error
}

var (
	ContainerProviders containerProviderRegistry
)

func init() {
	ContainerProviders.containerProviderMap = make(map[string]func() (ContainerProvider, error))
}

type containerProviderRegistry struct {
	containerProviderMap map[string]func() (ContainerProvider, error)
}

func (c containerProviderRegistry) RegisterProvider(name string, provider func() (ContainerProvider, error)) error {
	if _, ok := c.containerProviderMap[name]; ok == true {
		return fmt.Errorf("%v already registered as a provider", name)
	}

	c.containerProviderMap[name] = provider

	return nil
}

func (c containerProviderRegistry) findProvider(name *string) (func() (ContainerProvider, error), error) {
	glog.Infof("containerProviderMap = %+v", c.containerProviderMap)

	if provider, ok := c.containerProviderMap[*name]; ok == true {
		return provider, nil
	}

	return nil, fmt.Errorf("%v is an unknown provider", *name)
}

func NewContainerProvider(provider *string) (ContainerProvider, error) {
	glog.Infof("NewContainerProvider: enter")
	containerProvider, err := ContainerProviders.findProvider(provider)
	if err != nil {
		glog.Infof("findProvider failed")
		return nil, err
	}

	glog.Infof("calling init function")
	return containerProvider()
}
