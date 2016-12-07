package provider

import (
	"fmt"

	"github.com/sjpotter/infranetes/pkg/infranetes/provider/common"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

type PodProvider interface {
	RunPodSandbox(req *kubeapi.RunPodSandboxRequest) (*common.PodData, error)
	StopPodSandbox(podData *common.PodData)
	RemovePodSandbox(podData *common.PodData)
	PodSandboxStatus(podData *common.PodData)
	PreCreateContainer(*common.PodData, *kubeapi.CreateContainerRequest, func(req *kubeapi.ImageStatusRequest) (*kubeapi.ImageStatusResponse, error)) error
	ListInstances() ([]*common.PodData, error)

	UpdatePodState(podData *common.PodData)
}

type ImageProvider interface {
	ListImages(req *kubeapi.ListImagesRequest) (*kubeapi.ListImagesResponse, error)
	ImageStatus(req *kubeapi.ImageStatusRequest) (*kubeapi.ImageStatusResponse, error)
	PullImage(req *kubeapi.PullImageRequest) (*kubeapi.PullImageResponse, error)
	RemoveImage(req *kubeapi.RemoveImageRequest) (*kubeapi.RemoveImageResponse, error)
}

var (
	PodProviders   podProviderRegistry
	ImageProviders imgProviderRegistry
)

func init() {
	PodProviders.podProviderMap = make(map[string]func() (PodProvider, error))
	ImageProviders.imgProviderMap = make(map[string]func() (ImageProvider, error))
}

type podProviderRegistry struct {
	podProviderMap map[string]func() (PodProvider, error)
}

type imgProviderRegistry struct {
	imgProviderMap map[string]func() (ImageProvider, error)
}

func (p podProviderRegistry) RegisterProvider(name string, provider func() (PodProvider, error)) error {
	if _, ok := p.podProviderMap[name]; ok == true {
		return fmt.Errorf("%v already registered as a provider", name)
	}

	p.podProviderMap[name] = provider

	return nil
}

func (c imgProviderRegistry) RegisterProvider(name string, provider func() (ImageProvider, error)) error {
	if _, ok := c.imgProviderMap[name]; ok == true {
		return fmt.Errorf("%v already registered as a provider", name)
	}

	c.imgProviderMap[name] = provider

	return nil
}

func (p podProviderRegistry) findProvider(name string) (func() (PodProvider, error), error) {
	if provider, ok := p.podProviderMap[name]; ok == true {
		return provider, nil
	}

	return nil, fmt.Errorf("%v is an unknown provider", name)
}

func (c imgProviderRegistry) findProvider(name string) (func() (ImageProvider, error), error) {
	if provider, ok := c.imgProviderMap[name]; ok == true {
		return provider, nil
	}

	return nil, fmt.Errorf("%v is an unknown provider", name)
}

func NewPodProvider(provider string) (PodProvider, error) {
	podProvider, err := PodProviders.findProvider(provider)
	if err != nil {
		return nil, err
	}

	return podProvider()
}

func NewImageProvider(provider string) (ImageProvider, error) {
	imgProvider, err := ImageProviders.findProvider(provider)
	if err != nil {
		return nil, err
	}

	return imgProvider()
}
