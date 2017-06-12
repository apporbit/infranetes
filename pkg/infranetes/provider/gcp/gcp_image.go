package gcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"strings"
	"sync"

	"github.com/golang/glog"

	compute "google.golang.org/api/compute/v1"

	"github.com/sjpotter/infranetes/pkg/common/gcp"
	"github.com/sjpotter/infranetes/pkg/infranetes/provider"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1"
)

type gcpImageProvider struct {
	lock sync.RWMutex

	config   *gcp.GceConfig
	imageMap map[string]*kubeapi.Image
}

func init() {
	provider.ImageProviders.RegisterProvider("gcp", NewGCPImageProvider)
}

func NewGCPImageProvider() (provider.ImageProvider, error) {
	var conf gcp.GceConfig

	file, err := ioutil.ReadFile("gce.json")
	if err != nil {
		return nil, fmt.Errorf("File error: %v\n", err)
	}

	json.Unmarshal(file, &conf)

	if conf.SourceImage == "" || conf.Zone == "" || conf.Project == "" || conf.Scope == "" || conf.AuthFile == "" || conf.Network == "" || conf.Subnet == "" {
		msg := fmt.Sprintf("Failed to read in complete config file: conf = %+v", conf)
		glog.Info(msg)
		return nil, fmt.Errorf(msg)
	}

	provider := &gcpImageProvider{
		config:   &conf,
		imageMap: make(map[string]*kubeapi.Image),
	}

	return provider, nil
}

func (p *gcpImageProvider) ListImages(req *kubeapi.ListImagesRequest) (*kubeapi.ListImagesResponse, error) {
	p.lock.RLock()
	defer p.lock.RUnlock()

	result := []*kubeapi.Image{}

	if req.Filter != nil && req.Filter.Image != nil {
		if image, ok := p.imageMap[req.Filter.Image.Image]; ok {
			result = append(result, image)
		}
	} else {
		for _, image := range p.imageMap {
			result = append(result, image)
		}
	}

	resp := &kubeapi.ListImagesResponse{
		Images: result,
	}

	return resp, nil
}

func (p *gcpImageProvider) ImageStatus(req *kubeapi.ImageStatusRequest) (*kubeapi.ImageStatusResponse, error) {
	name := req.Image.Image

	if len(strings.Split(name, ":")) == 1 {
		name += ":latest"
		req.Image.Image = name
	}

	newreq := &kubeapi.ListImagesRequest{
		Filter: &kubeapi.ImageFilter{
			Image: req.Image,
		},
	}

	listresp, err := p.ListImages(newreq)
	if err != nil {
		return nil, err
	}

	switch len(listresp.Images) {
	case 0:
		return &kubeapi.ImageStatusResponse{}, nil
	case 1:
		return &kubeapi.ImageStatusResponse{Image: listresp.Images[0]}, nil
	default:
		return nil, fmt.Errorf("ImageStatus returned more than one image: %+v", listresp.Images)
	}
}

func toRuntimeAPIImage(image *compute.Image) (*kubeapi.Image, error) {
	if image == nil {
		return nil, errors.New("unable to convert a nil pointer to a runtime API image")
	}

	size := uint64(image.ArchiveSizeBytes)

	repoTag := image.Labels["infranetes-name"] + ":" + image.Labels["infranetes-version"]
	glog.Infof("RepoTag = %v", repoTag)

	return &kubeapi.Image{
		Id:          image.Name,
		RepoTags:    []string{repoTag},
		RepoDigests: []string{image.Name},
		Size_:       size,
	}, nil
}

func (p *gcpImageProvider) PullImage(req *kubeapi.PullImageRequest) (*kubeapi.PullImageResponse, error) {
	s, err := gcp.GetService(p.config.AuthFile, p.config.Project, p.config.Zone, []string{p.config.Scope})
	if err != nil {
		return nil, fmt.Errorf("PullImage: can't get gcp service %v", err)
	}

	splits := strings.Split(req.Image.Image, "/")
	var project string
	var fullname string
	switch len(splits) {
	case 1:
		project = s.Project
		fullname = splits[0]
		break
	case 2:
		project = splits[0]
		fullname = splits[1]
		break
	default:
		return nil, fmt.Errorf("PullImage: can't parse %v", req.Image.Image)
	}

	splits = strings.Split(fullname, ":")
	name := splits[0]
	var version string
	switch len(splits) {
	case 1:
		version = "latest"
		break
	case 2:
		version = splits[1]
		break
	default:
		return nil, fmt.Errorf("PullImage: can't parse %v", fullname)
	}

	glog.Infof("PullImage: Looking for name = %v and version = %v", name, version)

	nextPageToken := ""

	for {
		list, err := s.Service.Images.List(project).PageToken(nextPageToken).Do()
		if err != nil {
			return nil, fmt.Errorf("ListInstances failed: %v", err)
		}

		for _, i := range list.Items {
			glog.Infof("PullImage: image name = %v, image labels = %v", i.Name, i.Labels)
			if i.Labels["infranetes-name"] == name && i.Labels["infranetes-version"] == version {
				glog.Infof("PullImage: found with image %v", i.Name)
				p.lock.Lock()
				defer p.lock.Unlock()
				image, err := toRuntimeAPIImage(i)
				if err != nil {
					return nil, fmt.Errorf("PullImage: toRuntimeAPIImage failed: %v", err)
				}
				p.imageMap[req.Image.Image] = image

				return &kubeapi.PullImageResponse{ImageRef: i.Name}, nil
			}
			glog.Infof("skipped %v as %v != %v and $%v != %v", i.Name, name, i.Labels["infranetes-name"], version, i.Labels["infranetes-version"])
		}

		nextPageToken = list.NextPageToken

		if nextPageToken == "" {
			break
		}
	}

	return nil, fmt.Errorf("PullImage: couldn't find any image matching %v", req.Image.Image)
}

func (p *gcpImageProvider) RemoveImage(req *kubeapi.RemoveImageRequest) (*kubeapi.RemoveImageResponse, error) {
	p.lock.Lock()
	defer p.lock.Unlock()

	delete(p.imageMap, req.Image.Image)

	return &kubeapi.RemoveImageResponse{}, nil
}

func (p *gcpImageProvider) Integrate(pp provider.PodProvider) bool {
	switch pp.(type) {
	case *gcpPodProvider:
		app := pp.(*gcpPodProvider)
		//aws shouldn't boot on pod run if using container images
		app.imagePod = true

		return true
	}

	return false
}

func (p *gcpImageProvider) Translate(spec *kubeapi.ImageSpec) (string, error) {
	return spec.Image, nil
}
