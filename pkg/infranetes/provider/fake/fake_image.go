package fake

import (
	"fmt"

	"github.com/sjpotter/infranetes/pkg/infranetes/provider"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1"
)

type fakeImageProvider struct {
	imageList map[string]bool
}

func init() {
	provider.ImageProviders.RegisterProvider("fake", NewFakeImagerProvider)
}

func NewFakeImagerProvider() (provider.ImageProvider, error) {
	provider := &fakeImageProvider{
		imageList: make(map[string]bool, 0),
	}

	return provider, nil
}

func (p *fakeImageProvider) Integrate(pp provider.PodProvider) bool {
	return true
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
			Id: imageName,
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
