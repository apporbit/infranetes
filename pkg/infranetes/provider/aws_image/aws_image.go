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
	lock sync.Mutex
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

	return &awsImageProvider{}, nil
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
		RepoDigests: []string{image.ImageId},
		Size_:       &size,
	}, nil
}

func (p *awsImageProvider) ListImages(req *kubeapi.ListImagesRequest) (*kubeapi.ListImagesResponse, error) {
	p.lock.Lock()
	defer p.lock.Unlock()

	ec2Req := &ec2.DescribeImagesInput{}
	ec2Req.Owners = []*string{awsutil.String("self")}

	if req.Filter != nil && req.Filter.Image != nil {
		ec2Req.Filters = []*ec2.Filter{{Name: awsutil.String("tag:infranetes.image_name"), Values: []*string{req.Filter.Image.Image}}}
	}

	ec2Results, err := client.DescribeImages(ec2Req)
	if err != nil {
		return nil, fmt.Errorf("ListImages: ec2 DescribeImages failed: %v", err)
	}

	result := []*kubeapi.Image{}
	for _, image := range ec2Results.Images {
		apiImage, err := toRuntimeAPIImage(image)
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

func (p *awsImageProvider) ImageStatus(req *kubeapi.ImageStatusRequest) (*kubeapi.ImageStatusResponse, error) {
	glog.Info("aws ImageStatus: enter")
	defer glog.Info("aws ImageStatus: exit")

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

func (d *awsImageProvider) PullImage(req *kubeapi.PullImageRequest) (*kubeapi.PullImageResponse, error) {
	return nil, errors.New("PullImage: awsImageProvider doesn't support pulling images")
}

func (d *awsImageProvider) RemoveImage(req *kubeapi.RemoveImageRequest) (*kubeapi.RemoveImageResponse, error) {
	return nil, errors.New("RemoveImage: awsImageProvider doesn't removing pulling images")
}
