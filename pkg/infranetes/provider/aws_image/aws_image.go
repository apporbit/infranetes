package aws_image

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"strings"
	"sync"

	awsvm "github.com/apcera/libretto/virtualmachine/aws"
	awsutil "github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/golang/glog"

	"github.com/sjpotter/infranetes/pkg/infranetes/provider"
	infraws "github.com/sjpotter/infranetes/pkg/infranetes/provider/aws"
	"github.com/sjpotter/infranetes/pkg/infranetes/provider/common"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

var (
	client *ec2.EC2
)

type awsImageProvider struct {
	lock     sync.RWMutex
	imageMap map[string]*kubeapi.Image
}

func init() {
	provider.ImageProviders.RegisterProvider("aws", NewAWSImageProvider)
}

func NewAWSImageProvider() (provider.ImageProvider, error) {
	infraws.Boot = false

	var conf common.AwsConfig

	file, err := ioutil.ReadFile("aws.json")
	if err != nil {
		return nil, fmt.Errorf("File error: %v\n", err)
	}

	json.Unmarshal(file, &conf)

	if conf.Region == "" {
		msg := fmt.Sprintf("Failed to read in complete config file: conf = %+v", conf)
		glog.Info(msg)
		return nil, fmt.Errorf(msg)
	}

	if err := awsvm.ValidCredentials(conf.Region); err != nil {
		glog.Infof("Failed to Validated AWS Credentials")
		return nil, fmt.Errorf("failed to validate credentials: %v\n", err)
	}

	client = common.AwsGetClient(conf.Region)

	provider := &awsImageProvider{
		imageMap: make(map[string]*kubeapi.Image),
	}

	return provider, nil
}

func toRuntimeAPIImage(image *ec2.Image) (*kubeapi.Image, error) {
	if image == nil {
		return nil, errors.New("unable to convert a nil pointer to a runtime API image")
	}

	size := uint64(0)

	name := image.ImageId
	for _, tag := range image.Tags {
		if *tag.Key == "infranetes.image_name" {
			name = tag.Value
			break
		}
	}

	return &kubeapi.Image{
		Id:          image.ImageId,
		RepoTags:    []string{*name},
		RepoDigests: []string{*image.ImageId},
		Size_:       &size,
	}, nil
}

func (p *awsImageProvider) ListImages(req *kubeapi.ListImagesRequest) (*kubeapi.ListImagesResponse, error) {
	p.lock.RLock()
	defer p.lock.RUnlock()

	result := []*kubeapi.Image{}

	if req.Filter != nil && req.Filter.Image != nil {
		if image, ok := p.imageMap[*req.Filter.Image.Image]; ok {
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

func (p *awsImageProvider) ImageStatus(req *kubeapi.ImageStatusRequest) (*kubeapi.ImageStatusResponse, error) {
	name := *req.Image.Image

	if len(strings.Split(name, ":")) == 1 {
		name += ":latest"
		req.Image.Image = &name
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

func (p *awsImageProvider) PullImage(req *kubeapi.PullImageRequest) (*kubeapi.PullImageResponse, error) {
	ec2Req := &ec2.DescribeImagesInput{}

	splits := strings.Split(*req.Image.Image, "/")
	switch len(splits) {
	case 1:
		ec2Req.Owners = []*string{awsutil.String("self")}
		ec2Req.Filters = []*ec2.Filter{{Name: awsutil.String("tag:infranetes.image_name"), Values: []*string{&splits[0]}}}
		break
	case 2:
		ec2Req.Owners = []*string{awsutil.String(splits[0])}
		ec2Req.Filters = []*ec2.Filter{{Name: awsutil.String("tag:infranetes.image_name"), Values: []*string{&splits[1]}}}
		break
	default:
		return nil, fmt.Errorf("PullImage: can't parse %v", *req.Image.Image)
	}

	ec2Results, err := client.DescribeImages(ec2Req)
	if err != nil {
		return nil, fmt.Errorf("PullImage: ec2 DescribeImages failed: %v", err)
	}

	switch len(ec2Results.Images) {
	case 0:
		return nil, fmt.Errorf("PullImage: couldn't find any image matching %v", *req.Image.Image)
	case 1:
		p.lock.Lock()
		defer p.lock.Unlock()
		image, err := toRuntimeAPIImage(ec2Results.Images[0])
		if err != nil {
			return nil, fmt.Errorf("PullImage: toRuntimeAPIImage failed: %v", err)
		}
		p.imageMap[*req.Image.Image] = image

		return &kubeapi.PullImageResponse{}, nil
	default:
		return nil, fmt.Errorf("PullImage: ec2.DescribeImages returned more than one image: %+v", ec2Results.Images)
	}
}

func (p *awsImageProvider) RemoveImage(req *kubeapi.RemoveImageRequest) (*kubeapi.RemoveImageResponse, error) {
	p.lock.Lock()
	defer p.lock.Unlock()

	delete(p.imageMap, *req.Image.Image)

	return &kubeapi.RemoveImageResponse{}, nil
}
