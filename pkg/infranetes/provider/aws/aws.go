package aws

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sjpotter/infranetes/cmd/infranetes/flags"
	"github.com/sjpotter/infranetes/pkg/infranetes/provider"
	"github.com/sjpotter/infranetes/pkg/infranetes/provider/common"
	"github.com/sjpotter/infranetes/pkg/utils"

	"github.com/apcera/libretto/ssh"
	"github.com/apcera/libretto/virtualmachine/aws"
	"github.com/golang/glog"

	"github.com/aws/aws-sdk-go/aws/awserr"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

type podData struct {
	vmStateLastChecked time.Time
	id                 string
	podIp              string
}

type awsProvider struct {
	config *awsConfig
	ipList *utils.Deque
}

func init() {
	provider.PodProviders.RegisterProvider("aws", NewAWSProvider)
}

type awsConfig struct {
	Ami           string
	RouteTable    string
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

	ami := v.config.Ami
	if image, ok := req.Config.Annotations["infranetes.image"]; ok {
		glog.Infof("RunPodSandbox: overriding ami image with %v", image)
		ami = image
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
		Region:        v.config.Region,
		KeyPair:       strings.TrimSuffix(filepath.Base(v.config.SshKey), filepath.Ext(v.config.SshKey)),
		SecurityGroup: v.config.SecurityGroup,
		VPC:           v.config.Vpc,
		Subnet:        v.config.Subnet,
	}

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

	podIp := ips[index].String()

	name := vm.InstanceID

	client, err := common.CreateClient(podIp)
	if err != nil {
		return nil, fmt.Errorf("CreatePodSandbox: error in createClient(): %v", err)
	}

	err = client.StartProxy()
	if err != nil {
		client.Close()
		glog.Warningf("Couldn't start kube-proxy: %v", err)
	}

	podIp = v.ipList.Shift().(string)

	err = client.SetPodIP(podIp)

	/*	cmdReq := &vmserver.RunCmdRequest{}
		cmdReq.Cmd = "ifconfig"
		cmdReq.Args = []string{"eth0:0", "", "netmask", "255.255.255.255"}
		cmdReq.Args[1] = podIp

		glog.Infof("CreatePodSandbox: cmdReq = %+v", cmdReq)

		err = client.RunCmd(cmdReq) */
	if err != nil {
		glog.Warningf("CreatePodSandbox: Failed to configure inteface: %v", err)
	}

	err = client.SetSandboxConfig(req.Config)
	if err != nil {
		glog.Warningf("CreatePodSandbox: Failed to save sandbox config: %v", err)
	}

	err = destSourceReset(name)
	if err != nil {
		awsErr := err.(awserr.Error)
		glog.Warningf("CreatePodSandbox: destSourceReset failed: %v, code = %v, msg = %v", err.Error(), awsErr.Code(), awsErr.Message())
	}

	err = addRoute(v.config.RouteTable, name, podIp)
	if err != nil {
		awsErr := err.(awserr.Error)
		glog.Warningf("CreatePodSandbox: add route failed: %v, code = %v, msg = %v", err.Error(), awsErr.Code(), awsErr.Message())
	}

	providerData := &podData{
		vmStateLastChecked: time.Now(),
		id:                 name,
		podIp:              podIp,
	}

	podData := common.NewPodData(vm, &name, req.Config.Metadata, req.Config.Annotations, req.Config.Labels, podIp, req.Config.Linux, client, providerData)

	return podData, nil
}

func (v *awsProvider) StopPodSandbox(podData *common.PodData) {
}

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

func (v *awsProvider) PodSandboxStatus(podData *common.PodData) {
}

func (v *awsProvider) ListInstances() ([]*common.PodData, error) {
	glog.Infof("ListInstances: enter")
	instances, err := listInstances()
	if err != nil {
		return nil, err
	}

	podDatas := []*common.PodData{}
	for _, instance := range instances {
		podIp := *instance.PrivateIpAddress
		client, err := common.CreateClient(podIp)
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
