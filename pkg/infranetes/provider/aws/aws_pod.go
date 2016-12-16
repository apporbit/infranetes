package aws

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/golang/glog"

	"github.com/apcera/libretto/ssh"
	awsvm "github.com/apcera/libretto/virtualmachine/aws"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ec2"

	"github.com/sjpotter/infranetes/cmd/infranetes/flags"
	"github.com/sjpotter/infranetes/pkg/infranetes/provider"
	"github.com/sjpotter/infranetes/pkg/infranetes/provider/common"
	"github.com/sjpotter/infranetes/pkg/utils"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

type podData struct{}

type awsPodProvider struct {
	config *awsConfig
	ipList *utils.Deque
	amiPod bool
}

func init() {
	provider.PodProviders.RegisterProvider("aws", NewAWSPodProvider)
}

func NewAWSPodProvider() (provider.PodProvider, error) {
	var conf awsConfig

	file, err := ioutil.ReadFile("aws.json")
	if err != nil {
		return nil, fmt.Errorf("File error: %v\n", err)
	}

	json.Unmarshal(file, &conf)

	if conf.Ami == "" || conf.RouteTable == "" || conf.Region == "" || conf.SecurityGroup == "" || conf.Vpc == "" || conf.Subnet == "" || conf.SshKey == "" {
		msg := fmt.Sprintf("Failed to read in complete config file: conf = %+v", conf)
		glog.Info(msg)
		return nil, fmt.Errorf(msg)
	}

	glog.Infof("Validating AWS Credentials")

	if err := awsvm.ValidCredentials(conf.Region); err != nil {
		glog.Infof("Failed to Validated AWS Credentials")
		return nil, fmt.Errorf("failed to validate credentials: %v\n", err)
	}

	glog.Infof("Validated AWS Credentials")

	ipList := utils.NewDeque()
	for i := 1; i <= 255; i++ {
		ipList.Append(fmt.Sprint(*flags.IPBase + "." + strconv.Itoa(i)))
	}

	initEC2(conf.Region)

	return &awsPodProvider{
		config: &conf,
		ipList: ipList,
	}, nil
}

func (*awsPodProvider) UpdatePodState(data *common.PodData) {
	if data.Booted {
		data.UpdatePodState()
	}
}

func (p *awsPodProvider) bootSandbox(vm *awsvm.VM, config *kubeapi.PodSandboxConfig, podIp string) (*common.PodData, error) {
	if err := vm.Provision(); err != nil {
		return nil, fmt.Errorf("failed to provision vm: %v\n", err)
	}

	vm.SetTag("infranetes", "true")

	index := 1
	ips, err := vm.GetIPs()
	if err != nil {
		return nil, fmt.Errorf("CreatePodSandbox: error in GetIPs(): %v", err)
	}

	glog.Infof("CreatePodSandbox: ips = %v", ips)

	if ips[0] == nil {
		index = 1
	}

	ip := ips[index].String()

	name := vm.InstanceID

	client, err := common.CreateRealClient(ip)
	if err != nil {
		return nil, fmt.Errorf("CreatePodSandbox: error in createClient(): %v", err)
	}

	err = client.StartProxy()
	if err != nil {
		client.Close()
		glog.Warningf("CreatePodSandbox: Couldn't start kube-proxy: %v", err)
	}

	err = client.SetHostname(config.GetHostname())
	if err != nil {
		glog.Warningf("CreatePodSandbox: couldn't set hostname to %v: %v", config.GetHostname(), err)
	}

	err = client.SetPodIP(podIp)

	if err != nil {
		glog.Warningf("CreatePodSandbox: Failed to configure inteface: %v", err)
	}

	err = client.SetSandboxConfig(config)
	if err != nil {
		glog.Warningf("CreatePodSandbox: Failed to save sandbox config: %v", err)
	}

	err = destSourceReset(name)
	if err != nil {
		awsErr := err.(awserr.Error)
		glog.Warningf("CreatePodSandbox: destSourceReset failed: %v, code = %v, msg = %v", err.Error(), awsErr.Code(), awsErr.Message())
	}

	err = addRoute(p.config.RouteTable, name, podIp)
	if err != nil {
		awsErr := err.(awserr.Error)
		glog.Warningf("CreatePodSandbox: add route failed: %v, code = %v, msg = %v", err.Error(), awsErr.Code(), awsErr.Message())
	}

	providerData := &podData{}

	booted := true

	podData := common.NewPodData(vm, &podIp, config.Metadata, config.Annotations, config.Labels, podIp, config.Linux, client, booted, providerData)

	return podData, nil
}

func (v *awsPodProvider) RunPodSandbox(req *kubeapi.RunPodSandboxRequest) (*common.PodData, error) {
	rawKey, err := ioutil.ReadFile(v.config.SshKey)
	if err != nil {
		return nil, fmt.Errorf("failed to read key: %v\n", err)
	}

	ami := v.config.Ami
	if image, ok := req.Config.Annotations["infranetes.image"]; ok {
		glog.Infof("RunPodSandbox: overriding ami image with %v", image)
		ami = image
	}

	role := ""
	if iam, ok := req.Config.Annotations["infranetes.aws.iaminstancename"]; ok {
		glog.Infof("RunPodSandbox: booting instance iam role %v", iam)
		role = iam
	}

	awsName := req.Config.Metadata.GetNamespace() + ":" + req.Config.Metadata.GetName()
	vm := &awsvm.VM{
		Name:         awsName,
		AMI:          ami,
		InstanceType: "t2.micro",
		//		InstanceType: "m4.large",
		SSHCreds: ssh.Credentials{
			SSHUser:       "ubuntu",
			SSHPrivateKey: string(rawKey),
		},
		Volumes: []awsvm.EBSVolume{
			{
				DeviceName: "/dev/sda1",
			},
		},
		Region:                 v.config.Region,
		KeyPair:                strings.TrimSuffix(filepath.Base(v.config.SshKey), filepath.Ext(v.config.SshKey)),
		SecurityGroup:          v.config.SecurityGroup,
		Subnet:                 v.config.Subnet,
		IamInstanceProfileName: role,
	}

	podIp := v.ipList.Shift().(string)

	if !v.amiPod {
		return v.bootSandbox(vm, req.Config, podIp)
	} else {
		providerData := &podData{}

		client, err := common.CreateFakeClient()
		if err != nil { // Currently should be impossible to fail
			return nil, fmt.Errorf("")
		}

		booted := false

		podData := common.NewPodData(vm, &podIp, req.Config.Metadata, req.Config.Annotations, req.Config.Labels, podIp, req.Config.Linux, client, booted, providerData)

		return podData, nil
	}
}

func (v *awsPodProvider) PreCreateContainer(data *common.PodData, req *kubeapi.CreateContainerRequest, imageStatus func(req *kubeapi.ImageStatusRequest) (*kubeapi.ImageStatusResponse, error)) error {
	data.BootLock.Lock()
	defer data.BootLock.Unlock()

	// This function is really only for when amiPod == true and this pod hasn't been booted yet (i.e. only one container)
	// The below check enforces that.  Errors out if more than one "container" is used for an amiPod and just returns if not an amiPod
	if v.amiPod == true {
		if data.Booted {
			msg := "Trying to launch another container into a virtual machine"
			glog.Infof("PreCreateContainer: %v", msg)
			return errors.New(msg)
		}
	} else {
		glog.Info("PreCreateContainer: shortcutting as not an amiPod")
		return nil
	}

	// Image Case
	vm, ok := data.VM.(*awsvm.VM)
	if !ok {
		return errors.New("PreCreateContainer: podData's VM wasn't an aws VM struct")
	}

	result, err := imageStatus(&kubeapi.ImageStatusRequest{Image: req.Config.Image})
	if err == nil && len(result.Image.RepoTags) == 1 {
		glog.Infof("PreCreateContainer: translated %v to %v", *req.Config.Image.Image, *result.Image.Id)
		vm.AMI = *result.Image.Id
	} else {
		return fmt.Errorf("PreCreateContainer: Couldn't translate %v: err = %v and result = %v", *req.Config.Image.Image, err, result)
	}

	newPodData, err := v.bootSandbox(vm, req.SandboxConfig, data.Ip)
	if err != nil {
		return fmt.Errorf("PreCreateContainer: couldn't boot VM: %v", err)
	}

	data.Booted = true

	data.Client = newPodData.Client
	data.ProviderData = newPodData.ProviderData

	return nil
}

func (v *awsPodProvider) StopPodSandbox(podData *common.PodData) {}

func (v *awsPodProvider) RemovePodSandbox(data *common.PodData) {
	glog.Infof("RemovePodSandbox: release IP: %v", data.Ip)

	err := delRoute(v.config.RouteTable, data.Ip)
	if err != nil {
		awsErr := err.(awserr.Error)
		glog.Warningf("RemovePodSandbox: delRoute failed: %v, code = %v, msg = %v", err.Error(), awsErr.Code(), awsErr.Message())
	} else {
		v.ipList.Append(data.Ip)
	}
}

func (v *awsPodProvider) PodSandboxStatus(podData *common.PodData) {}

func listInstances() ([]*ec2.Instance, error) {
	filters := []*ec2.Filter{
		{
			Name:   aws.String("instance-state-name"),
			Values: []*string{aws.String("running"), aws.String("pending")},
		},
	}

	request := ec2.DescribeInstancesInput{Filters: filters}
	result, err := client.DescribeInstances(&request)
	if err != nil {
		return nil, err
	}

	instances := []*ec2.Instance{}

	for _, resv := range result.Reservations {
		for _, instance := range resv.Instances {
			for _, tag := range instance.Tags {
				if "infranetes" == *tag.Key {
					instances = append(instances, instance)
				}
			}
		}
	}

	return instances, nil
}

func (v *awsPodProvider) ListInstances() ([]*common.PodData, error) {
	glog.Infof("ListInstances: enter")
	instances, err := listInstances()
	if err != nil {
		return nil, err
	}

	podDatas := []*common.PodData{}
	for _, instance := range instances {
		podIp := *instance.PrivateIpAddress
		client, err := common.CreateRealClient(podIp)
		if err != nil {
			return nil, fmt.Errorf("CreatePodSandbox: error in createClient(): %v", err)
		}

		podIp, err = client.GetPodIP()
		if err != nil {
			continue
		}

		config, err := client.GetSandboxConfig()
		if err != nil {
			continue
		}

		name := podIp

		vm := &awsvm.VM{
			InstanceID: *instance.InstanceId,
			Region:     v.config.Region,
		}

		providerData := &podData{}

		v.ipList.FindAndRemove(podIp)

		glog.Infof("ListInstances: creating a podData for %v", name)
		booted := true
		podData := common.NewPodData(vm, &name, config.Metadata, config.Annotations, config.Labels, podIp, config.Linux, client, booted, providerData)

		podDatas = append(podDatas, podData)
	}

	return podDatas, nil
}
