package fake

import (
	"fmt"
	"strconv"

	"github.com/sjpotter/infranetes/cmd/infranetes/flags"
	"github.com/sjpotter/infranetes/pkg/infranetes/provider"
	"github.com/sjpotter/infranetes/pkg/infranetes/provider/common"
	"github.com/sjpotter/infranetes/pkg/utils"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

type fakeImageProvider struct {
	imageList map[string]bool
}

type fakePodProvider struct {
	instances map[string]*common.PodData
	ipList    *utils.Deque
}

func init() {
	provider.ImageProviders.RegisterProvider("fake", NewFakeImagerProvider)
	provider.PodProviders.RegisterProvider("fake", NewFakePodProvider)
}

func NewFakeImagerProvider() (provider.ImageProvider, error) {
	provider := &fakeImageProvider{
		imageList: make(map[string]bool, 0),
	}

	return provider, nil
}

func NewFakePodProvider() (provider.PodProvider, error) {
	ipList := utils.NewDeque()
	for i := 1; i <= 255; i++ {
		ipList.Append(fmt.Sprint(*flags.IPBase + "." + strconv.Itoa(i)))
	}

	provider := &fakePodProvider{
		instances: make(map[string]*common.PodData),
		ipList:    ipList,
	}

	return provider, nil
}

func (p *fakePodProvider) RunPodSandbox(req *kubeapi.RunPodSandboxRequest) (*common.PodData, error) {
	name := "fake-" + utils.RandString(10)
	vm := &fakeVM{
		name: name,
	}

	client, _ := common.CreateFakeClient()
	podIp := p.ipList.Shift().(string)

	podData := common.NewPodData(vm, &vm.name, req.Config.Metadata, req.Config.Annotations, req.Config.Labels, podIp, req.Config.Linux, client, nil)

	p.instances[name] = podData

	return podData, nil
}

func (*fakePodProvider) UpdatePodState(cPodData *common.PodData) {}

func (v *fakePodProvider) StopPodSandbox(podData *common.PodData) {}

func (v *fakePodProvider) RemovePodSandbox(data *common.PodData) {
	// putting ip back into queue
	v.ipList.Append(data.Ip)
}

func (v *fakePodProvider) PodSandboxStatus(podData *common.PodData) {}

func (v *fakePodProvider) ListInstances() ([]*common.PodData, error) {
	return nil, nil
}

func (p *fakeImageProvider) PullImage(req *kubeapi.PullImageRequest) (*kubeapi.PullImageResponse, error) {
	p.imageList[req.GetImage().GetImage()] = true

	return &kubeapi.PullImageResponse{}, nil
}

func (p *fakeImageProvider) ListImages(req *kubeapi.ListImagesRequest) (*kubeapi.ListImagesResponse, error) {
	result := []*kubeapi.Image{}

	for imageName := range p.imageList {
		if req.Filter != nil && req.Filter.GetImage().GetImage() != imageName {
			continue
		}

		image := &kubeapi.Image{
			Id: &imageName,
		}

		result = append(result, image)
	}

	resp := &kubeapi.ListImagesResponse{
		Images: result,
	}

	return resp, nil
}

func (p *fakeImageProvider) ImageStatus(req *kubeapi.ImageStatusRequest) (*kubeapi.ImageStatusResponse, error) {
	newreq := &kubeapi.ListImagesRequest{
		Filter: &kubeapi.ImageFilter{
			Image: req.Image,
		},
	}
	listresp, err := p.ListImages(newreq)
	if err != nil {
		return nil, err
	}
	images := listresp.Images
	if len(images) > 1 {
		return nil, fmt.Errorf("ImageStatus returned more than one image: %+v", images)
	}

	if len(images) == 0 {
		return &kubeapi.ImageStatusResponse{}, nil
	}

	resp := &kubeapi.ImageStatusResponse{
		Image: images[0],
	}
	return resp, nil
}

func (p *fakeImageProvider) RemoveImage(req *kubeapi.RemoveImageRequest) (*kubeapi.RemoveImageResponse, error) {
	delete(p.imageList, req.GetImage().GetImage())

	return &kubeapi.RemoveImageResponse{}, nil
}
