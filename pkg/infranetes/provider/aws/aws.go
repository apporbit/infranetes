package aws

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/golang/glog"

	"github.com/apcera/libretto/ssh"
	"github.com/apcera/libretto/virtualmachine/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"

	"github.com/sjpotter/infranetes/cmd/infranetes/flags"
	"github.com/sjpotter/infranetes/pkg/infranetes/provider"
	"github.com/sjpotter/infranetes/pkg/infranetes/provider/common"
	"github.com/sjpotter/infranetes/pkg/utils"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

type podData struct {
	vmStateLastChecked time.Time
	id                 string
	podIp              string
	booted             bool
}

type awsProvider struct {
	config *common.AwsConfig
	ipList *utils.Deque
}

func init() {
	provider.PodProviders.RegisterProvider("aws", NewAWSProvider)
}

var (
	Boot = true
)

func NewAWSProvider() (provider.PodProvider, error) {
	var conf common.AwsConfig

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

	if err := aws.ValidCredentials(conf.Region); err != nil {
		glog.Infof("Failed to Validated AWS Credentials")
		return nil, fmt.Errorf("failed to validate credentials: %v\n", err)
	}

	glog.Infof("Validated AWS Credentials")

	ipList := utils.NewDeque()
	for i := 1; i <= 255; i++ {
		ipList.Append(fmt.Sprint(*flags.IPBase + "." + strconv.Itoa(i)))
	}

	InitEC2(conf.Region)

	return &awsProvider{
		config: &conf,
		ipList: ipList,
	}, nil
}

func (*awsProvider) UpdatePodState(data *common.PodData) {
	providerData, ok := data.ProviderData.(*podData)
	if !ok {
		glog.Warningf("UpdateVMState: Couldn't get ProviderData")
		return
	}

	if providerData.booted {
		if time.Now().After(providerData.vmStateLastChecked.Add(30 * time.Second)) {
			err := data.UpdatePodState()
			if err == nil {
				providerData.vmStateLastChecked = time.Now()
			}
		}
	}
}

func (p *awsProvider) bootSandbox(vm *aws.VM, config *kubeapi.PodSandboxConfig, podIp string) (*common.PodData, error) {
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

	providerData := &podData{
		vmStateLastChecked: time.Now(),
		id:                 name,
		podIp:              podIp,
		booted:             true,
	}

	podData := common.NewPodData(vm, &podIp, config.Metadata, config.Annotations, config.Labels, podIp, config.Linux, client, providerData)

	return podData, nil
}

func (v *awsProvider) RunPodSandbox(req *kubeapi.RunPodSandboxRequest) (*common.PodData, error) {
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
	vm := &aws.VM{
		Name:         awsName,
		AMI:          ami,
		InstanceType: "t2.micro",
		//		InstanceType: "m4.large",
		SSHCreds: ssh.Credentials{
			SSHUser:       "ubuntu",
			SSHPrivateKey: string(rawKey),
		},
		Volumes: []aws.EBSVolume{
			{
				DeviceName: "/dev/sda1",
			},
		},
		Region:                 v.config.Region,
		KeyPair:                strings.TrimSuffix(filepath.Base(v.config.SshKey), filepath.Ext(v.config.SshKey)),
		SecurityGroup:          v.config.SecurityGroup,
		VPC:                    v.config.Vpc,
		Subnet:                 v.config.Subnet,
		IamInstanceProfileName: role,
	}

	podIp := v.ipList.Shift().(string)

	if Boot {
		return v.bootSandbox(vm, req.Config, podIp)
	} else {
		providerData := &podData{
			podIp:  podIp,
			booted: false,
		}

		client, _ := common.CreateFakeClient()

		podData := common.NewPodData(vm, &podIp, req.Config.Metadata, req.Config.Annotations, req.Config.Labels, podIp, req.Config.Linux, client, providerData)

		return podData, nil
	}
}

func (v *awsProvider) PreCreateContainer(data *common.PodData, req *kubeapi.CreateContainerRequest, imageStatus func(req *kubeapi.ImageStatusRequest) (*kubeapi.ImageStatusResponse, error)) error {
	// Pod Case
	providerData := data.ProviderData.(*podData)
	if providerData.booted {
		return nil
	}

	// Image Case
	vm, ok := data.VM.(*aws.VM)
	if !ok {
		return errors.New("PreCreateContainer: podData's VM wasn't an aws VM struct")
	}

	var image string

	result, err := imageStatus(&kubeapi.ImageStatusRequest{Image: req.Config.Image})
	if err == nil && len(result.Image.RepoTags) == 1 {
		glog.Infof("PreCreateContainer: translated %v to %v", *req.Config.Image.Image, *result.Image.Id)
		image = *result.Image.Id
	} else {
		return fmt.Errorf("PreCreateContainer: Couldn't translate %v: err = %v and result = %v", *req.Config.Image.Image, err, result)
	}

	splits := strings.Split(image, ":")
	vm.AMI = splits[0]

	newPodData, err := v.bootSandbox(vm, req.SandboxConfig, data.Ip)
	if err != nil {
		return fmt.Errorf("PreCreateContainer: couldn't boot VM: %v", err)
	}

	data.Client = newPodData.Client
	data.ProviderData = newPodData.ProviderData

	return nil
}

func (v *awsProvider) StopPodSandbox(podData *common.PodData) {}

func (v *awsProvider) RemovePodSandbox(data *common.PodData) {
	providerData := data.ProviderData.(*podData)
	glog.Infof("RemovePodSandbox: release IP: %v", providerData.podIp)
	v.ipList.Append(providerData.podIp)

	err := delRoute(v.config.RouteTable, providerData.podIp)
	if err != nil {
		awsErr := err.(awserr.Error)
		glog.Warningf("RemovePodSandbox: delRoute failed: %v, code = %v, msg = %v", err.Error(), awsErr.Code(), awsErr.Message())
	}
}

func (v *awsProvider) PodSandboxStatus(podData *common.PodData) {}

func (v *awsProvider) ListInstances() ([]*common.PodData, error) {
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

		name := *instance.InstanceId

		vm := &aws.VM{
			InstanceID: *instance.InstanceId,
			Region:     v.config.Region,
		}

		providerData := &podData{
			vmStateLastChecked: time.Now(),
			id:                 name,
			podIp:              podIp,
		}

		v.ipList.FindAndRemove(podIp)

		glog.Infof("ListInstances: creating a podData for %v", name)
		podData := common.NewPodData(vm, &name, config.Metadata, config.Annotations, config.Labels, podIp, config.Linux, client, providerData)

		podDatas = append(podDatas, podData)
	}

	return podDatas, nil
}
