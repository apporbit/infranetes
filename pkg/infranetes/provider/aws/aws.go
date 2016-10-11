package aws

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"
	"time"

	"github.com/sjpotter/infranetes/pkg/infranetes/provider"
	"github.com/sjpotter/infranetes/pkg/infranetes/provider/common"

	"github.com/apcera/libretto/ssh"
	"github.com/apcera/libretto/virtualmachine/aws"
	"github.com/golang/glog"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

type podData struct {
	vmStateLastChecked time.Time
}

type awsProvider struct {
	config *awsConfig
}

func init() {
	provider.PodProviders.RegisterProvider("aws", NewAWSProvider)
}

type awsConfig struct {
	Ami           string
	Region        string
	SecurityGroup string
	Vpc           string
	Subnet        string
	SshKey        string
}

func NewAWSProvider() (provider.PodProvider, error) {
	var conf awsConfig

	file, err := ioutil.ReadFile("aws.json")
	if err != nil {
		return nil, fmt.Errorf("File error: %v\n", err)
	}

	json.Unmarshal(file, &conf)

	glog.Infof("Validating AWS Credentials")

	if err := aws.ValidCredentials("use-west-2"); err != nil {
		glog.Infof("Failed to Validated AWS Credentials")
		return nil, fmt.Errorf("failed to validate credentials: %v\n", err)
	}

	glog.Infof("Validated AWS Credentials")

	return &awsProvider{
		config: &conf,
	}, nil
}

func (*awsProvider) UpdatePodState(cPodData *common.PodData) {
	podData, ok := cPodData.ProviderData.(*podData)
	if !ok {
		glog.Warningf("UpdateVMState: Couldn't get ProviderData")
		return
	}

	if time.Now().After(podData.vmStateLastChecked.Add(30 * time.Second)) {
		err := cPodData.UpdatePodState()
		if err == nil {
			podData.vmStateLastChecked = time.Now()
		}
	}
}

func (v *awsProvider) RunPodSandbox(req *kubeapi.RunPodSandboxRequest) (*common.PodData, error) {
	rawKey, err := ioutil.ReadFile(v.config.SshKey)
	if err != nil {
		return nil, fmt.Errorf("failed to read key: %v\n", err)
	}

	awsName := req.Config.Metadata.GetNamespace() + ":" + req.Config.Metadata.GetName()
	vm := &aws.VM{
		Name:         awsName,
		AMI:          v.config.Ami,
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
		Region:        v.config.Region,
		KeyPair:       strings.TrimSuffix(filepath.Base(v.config.SshKey), filepath.Ext(v.config.SshKey)),
		SecurityGroup: v.config.SecurityGroup,
		VPC:           v.config.Vpc,
		Subnet:        v.config.Subnet,
	}

	if err := vm.Provision(); err != nil {
		return nil, fmt.Errorf("failed to provision vm: %v\n", err)
	}

	ips, err := vm.GetIPs()
	if err != nil {
		return nil, fmt.Errorf("CreatePodSandbox: error in GetIPs(): %v", err)
	}

	ip := ips[0].String()

	name := vm.InstanceID

	client, err := common.CreateClient(ip)
	if err != nil {
		return nil, fmt.Errorf("CreatePodSandbox: error in createClient(): %v", err)
	}

	providerData := &podData{
		vmStateLastChecked: time.Now(),
	}

	podData := common.NewPodData(vm, &name, req.Config.Metadata, req.Config.Annotations, req.Config.Labels, ip, client, providerData)

	return podData, nil
}

func (v *awsProvider) StopPodSandbox(podData *common.PodData) {
}

func (v *awsProvider) RemovePodSandbox(podData *common.PodData) {
}

func (v *awsProvider) PodSandboxStatus(podData *common.PodData) {
}
