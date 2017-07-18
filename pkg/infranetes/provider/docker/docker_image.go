package docker

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	dockerclient "github.com/docker/engine-api/client"
	dockertypes "github.com/docker/engine-api/types"

	"github.com/apporbit/infranetes/pkg/common"
	"github.com/apporbit/infranetes/pkg/infranetes/provider"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1/runtime"
)

type dockerImageProvider struct {
	client   *dockerclient.Client
	imageMap map[string]string
}

func init() {
	provider.ImageProviders.RegisterProvider("docker", NewDockerImageProvider)
}

func NewDockerImageProvider() (provider.ImageProvider, error) {
	if client, err := dockerclient.NewClient(dockerclient.DefaultDockerHost, "", nil, nil); err != nil {
		return nil, err
	} else {
		dockerImageProvider := &dockerImageProvider{
			client:   client,
			imageMap: make(map[string]string),
		}

		return dockerImageProvider, nil
	}
}

func (d *dockerImageProvider) ListImages(req *kubeapi.ListImagesRequest) (*kubeapi.ListImagesResponse, error) {
	opts := dockertypes.ImageListOptions{}

	filter := req.Filter
	if filter != nil {
		if imgSpec := filter.GetImage(); imgSpec != nil {
			opts.MatchName = imgSpec.GetImage()
		}
	}

	images, err := d.client.ImageList(context.Background(), opts)
	if err != nil {
		return nil, err
	}

	result := []*kubeapi.Image{}
	for _, i := range images {
		apiImage, err := common.ToRuntimeAPIImage(&i)
		if err != nil {
			// TODO: log an error message?
			continue
		}
		result = append(result, apiImage)
	}

	resp := &kubeapi.ListImagesResponse{
		Images: result,
	}

	return resp, nil
}

func (d *dockerImageProvider) ImageStatus(req *kubeapi.ImageStatusRequest) (*kubeapi.ImageStatusResponse, error) {
	newreq := &kubeapi.ListImagesRequest{
		Filter: &kubeapi.ImageFilter{
			Image: req.Image,
		},
	}
	listresp, err := d.ListImages(newreq)
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

	d.imageMap[resp.Image.Id] = req.Image.Image

	return resp, nil
}

func (d *dockerImageProvider) PullImage(req *kubeapi.PullImageRequest) (*kubeapi.PullImageResponse, error) {
	pullresp, err := d.client.ImagePull(context.Background(), req.Image.GetImage(), dockertypes.ImagePullOptions{})
	if err != nil {
		return nil, fmt.Errorf("ImagePull Failed (%v)\n", err)
	}

	decoder := json.NewDecoder(pullresp)
	for {
		var msg interface{}
		err := decoder.Decode(&msg)

		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("Pull Image failed: %v", err)
		}
	}

	pullresp.Close()

	resp := &kubeapi.PullImageResponse{}

	return resp, err
}

func (d *dockerImageProvider) RemoveImage(req *kubeapi.RemoveImageRequest) (*kubeapi.RemoveImageResponse, error) {
	_, err := d.client.ImageRemove(context.Background(), req.Image.GetImage(), dockertypes.ImageRemoveOptions{PruneChildren: true})

	resp := &kubeapi.RemoveImageResponse{}

	return resp, err
}

func (d *dockerImageProvider) Integrate(pp provider.PodProvider) bool {
	return true
}

func (d *dockerImageProvider) Translate(spec *kubeapi.ImageSpec) (string, error) {
	if ret, ok := d.imageMap[spec.Image]; ok {
		return ret, nil
	}

	msg := fmt.Sprintf("Translate: Couldn't Translate: %v", spec.Image)
	glog.Info(msg)
	return "", errors.New(msg)
}
