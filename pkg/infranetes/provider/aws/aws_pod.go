package aws

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang/glog"

	"github.com/apcera/libretto/ssh"
	awsvm "github.com/apcera/libretto/virtualmachine/aws"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ec2"

	"github.com/apporbit/infranetes/cmd/infranetes/flags"
	"github.com/apporbit/infranetes/pkg/infranetes/provider"
	"github.com/apporbit/infranetes/pkg/infranetes/provider/common"
	"github.com/apporbit/infranetes/pkg/infranetes/types"
	"github.com/apporbit/infranetes/pkg/utils"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1/runtime"
)

type podData struct {
	instanceId  *string
	usedDevices map[string]bool
	attached    map[string]string
	lock        sync.Mutex
	volumes     []*types.Volume
}

type awsPodProvider struct {
	config   *awsConfig
	ipList   *utils.Deque
	imagePod bool
	key      string
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

	rawKey, err := ioutil.ReadFile(conf.SshKey)
	if err != nil {
		return nil, fmt.Errorf("failed to read key: %v\n", err)
	}

	initEC2(conf.Region)

	// FIXME: probably want to pull out ip handling into a "network plugin", would want to verify boot image supports plugin
	// Currently: this just controls allocation to an independent infranetes subnet, L3 routing has to be setup correctly on cloud
	// Enable autodetection of infranetes ip range
	if *flags.IPBase == "" {
		base, err := findBase(&conf.Subnet)
		if err != nil {
			msg := fmt.Sprintf("findBase failed: %v", err)
			glog.Errorf(msg)
			return nil, errors.New(msg)
		}
		flags.IPBase = base
	}

	if *flags.MasterIP == "" {
		masterIP, ok := findMaster()
		if ok != true {
			msg := fmt.Sprintf("Couldn't find kube master ip")
			glog.Error(msg)
			return nil, errors.New(msg)
		}
		flags.MasterIP = masterIP
	}

	ipList := utils.NewDeque()
	// AWS VPC reserved .1->.3 and .255
	for i := 4; i < 255; i++ {
		ipList.Append(fmt.Sprint(*flags.IPBase + "." + strconv.Itoa(i)))
	}

	return &awsPodProvider{
		config: &conf,
		ipList: ipList,
		key:    string(rawKey),
	}, nil
}

func (*awsPodProvider) UpdatePodState(data *common.PodData) {
	if data.Booted {
		data.UpdatePodState()
	}
}

// FIXME: if steps fail, probably want to teardown VM
func (p *awsPodProvider) bootSandbox(vm *awsvm.VM, config *kubeapi.PodSandboxConfig, name string, volumes []*types.Volume) (*common.PodData, error) {
	// 1. Parse Annotations from PodSandboxConfig
	cAnno := common.ParseCommonAnnotations(config.Annotations)

	// 2. Boot VM
	if err := vm.Provision(); err != nil {
		return nil, fmt.Errorf("failed to provision vm: %v\n", err)
	}

	vm.SetTag("infranetes", "true")

	// 3. Extract IP Info
	ips, err := vm.GetIPs()
	if err != nil {
		return nil, fmt.Errorf("bootSandbox: error in GetIPs(): %v", err)
	}

	glog.Infof("bootSandbox: ips = %v", ips)

	// FIXME: Perhaps better way to choose public vs private ip
	index := 1
	podIp := ips[index].String()

	glog.Infof("bootSandbox: podIp = %v", podIp)

	// 4. Connect to VMServer in VM
	client, err := common.CreateRealClient(podIp)
	if err != nil {
		return nil, fmt.Errorf("bootSandbox: error in createClient(): %v", err)
	}

	providerData := &podData{
		instanceId:  &vm.InstanceID,
		usedDevices: make(map[string]bool),
		attached:    make(map[string]string),
		volumes:     volumes,
	}

	// 5. Attach/Mount EBS Volumes
	for _, vol := range volumes {
		device, err := providerData.Attach(vol.Volume, vol.Device)
		if err != nil {
			glog.Warningf("bootSandbox: failed to attach %v to %v in %v", vol.Volume, device, vm.InstanceID)
		} else {
			if vol.MountPoint != "" {
				err := client.MountFs(device, vol.MountPoint, vol.FsType, vol.ReadOnly)
				if err != nil {
					glog.Warningf("bootSandbox: failed to mount %v(%v) on %v in %v", vol.Volume, device, vol.MountPoint, vm.InstanceID)
				}
			}
		}
	}

	// 6. Setup Instaunce / VM Correctly
	// Store Config so can be recovered if neccessary
	err = client.SetSandboxConfig(config)
	if err != nil {
		glog.Warningf("CreatePodSandbox: Failed to save sandbox config: %v", err)
	}

	err = client.SetPodIP(podIp)
	if err != nil {
		glog.Warningf("CreatePodSandbox: Failed to configure inteface: %v", err)
	}

	// Do we start kube-proxy?
	if cAnno.StartProxy {
		err = client.StartProxy()
		if err != nil {
			client.Close()
			glog.Warningf("CreatePodSandbox: Couldn't start kube-proxy: %v", err)
		}
	} else {
		glog.Infof("CreatePodSandbox: Skipping Proxy")
	}

	// Do we set the hostname to the pod's name
	if cAnno.SetHostname {
		err = client.SetHostname(config.GetHostname())
		if err != nil {
			glog.Warningf("CreatePodSandbox: couldn't set hostname to %v: %v", config.GetHostname(), err)
		}
	} else {
		glog.Infof("CreatePodSandbox: Skipping changing hostname")
	}

	booted := true

	podData := common.NewPodData(vm, name, config.Metadata, config.Annotations, config.Labels, podIp, config.Linux, client, booted, providerData)

	return podData, nil
}

func (v *awsPodProvider) RunPodSandbox(req *kubeapi.RunPodSandboxRequest, volumes []*types.Volume) (*common.PodData, error) {
	podIp := v.ipList.Shift().(string)

	vm := v.createVM(req.Config, podIp)

	if !v.imagePod { // Traditional Pod, but within a VM
		ret, err := v.bootSandbox(vm, req.Config, podIp, volumes)

		if err == nil { //i.e. boot succeeded
			handleElasticIP(req.Config, vm.GetName())
		}

		return ret, err
	} else { // Booting a VM immage to appear as a Pod to K8s.  Can't boot it until container time
		//FIXME: make generic later
		providerData := &podData{volumes: volumes}

		client, err := common.CreateFakeClient()
		if err != nil { // Currently should be impossible to fail
			return nil, err
		}

		booted := false

		podData := common.NewPodData(vm, podIp, req.Config.Metadata, req.Config.Annotations, req.Config.Labels, podIp, req.Config.Linux, client, booted, providerData)

		return podData, nil
	}
}

// FIXME: if booting a VM here fails, do we want to fail the whole pod?
func (v *awsPodProvider) PreCreateContainer(data *common.PodData, req *kubeapi.CreateContainerRequest, imageStatus func(req *kubeapi.ImageStatusRequest) (*kubeapi.ImageStatusResponse, error)) error {
	data.BootLock.Lock()
	defer data.BootLock.Unlock()

	// FIXME: this should be made something that is passed into function later so don't have to do this for every pod provider
	var volumes []*types.Volume
	if providerData, ok := data.ProviderData.(*podData); ok {
		volumes = providerData.volumes
	}

	// This function is really only for when amiPod == true and this pod hasn't been booted yet (i.e. only one container)
	// The below check enforces that.  Errors out if more than one "container" is used for an amiPod and just returns if not an amiPod
	if v.imagePod == true {
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

// Old code
/*	result, err := imageStatus(&kubeapi.ImageStatusRequest{Image: req.Config.Image})
	if err == nil && result.Image != nil {
		glog.Infof("PreCreateContainer: translated %v to %v", req.Config.Image.Image, result.Image.Id)
		vm.AMI = result.Image.Id
	} else {
		return fmt.Errorf("PreCreateContainer: Couldn't translate %v: err = %v and result = %v", req.Config.Image.Image, err, result)
	}
*/
	// Don't need to convert, getting the AMI here
	vm.AMI = req.Config.Image.Image

	newPodData, err := v.bootSandbox(vm, req.SandboxConfig, data.Ip, volumes)
	if err != nil {
		return fmt.Errorf("PreCreateContainer: couldn't boot VM: %v", err)
	}

	handleElasticIP(req.GetSandboxConfig(), vm.GetName())

	data.Booted = true

	data.Client = newPodData.Client
	data.ProviderData = newPodData.ProviderData

	return nil
}

func (v *awsPodProvider) StopPodSandbox(pdata *common.PodData) {
	providerData, ok := pdata.ProviderData.(*podData)
	providerData.lock.Lock()
	defer providerData.lock.Unlock()

	if !ok {
		glog.Warningf("StopPodSandbox: couldn't type assert ProviderData to podData")
		return
	}

	for _, vol := range providerData.volumes {
		if vol.MountPoint != "" {
			err := pdata.Client.UnmountFs(vol.MountPoint)
			if err != nil {
				glog.Warningf("StopPodSandbox: couldn't unmount %v on %v", vol.MountPoint, *providerData.instanceId)
			}
		}
		err := providerData.detach(vol.Volume, true)
		if err != nil {
			glog.Warningf("StopPodSandbox: couldn't detach %v from %v", vol.Volume, *providerData.instanceId)
		}
	}

	providerData.volumes = nil
}

func (v *awsPodProvider) RemovePodSandbox(data *common.PodData) {
	glog.Infof("RemovePodSandbox: release IP: %v", data.Ip)

	v.ipList.Append(data.Ip)
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
		podData := common.NewPodData(vm, name, config.Metadata, config.Annotations, config.Labels, podIp, config.Linux, client, booted, providerData)

		podDatas = append(podDatas, podData)
	}

	return podDatas, nil
}

func (v *awsPodProvider) createVM(config *kubeapi.PodSandboxConfig, podIp string) *awsvm.VM {
	aAnno := parseAWSAnnotations(config.Annotations)

	vm := &awsvm.VM{
		AMI:              v.config.Ami,
		InstanceType:     "t2.micro",
		Region:           v.config.Region,
		KeyPair:          strings.TrimSuffix(filepath.Base(v.config.SshKey), filepath.Ext(v.config.SshKey)),
		SecurityGroups:   []string{v.config.SecurityGroup},
		Subnet:           v.config.Subnet,
		PrivateIPAddress: podIp,

		Volumes: []awsvm.EBSVolume{
			{
				DeviceName: "/dev/sda1",
			},
		},
		SSHCreds: ssh.Credentials{
			SSHUser:       "ubuntu",
			SSHPrivateKey: v.key,
		},
	}

	// Fill in VM struct with data from annotations if required
	overrideVMDefault(vm, aAnno)

	return vm
}

func handleElasticIP(config *kubeapi.PodSandboxConfig, name string) {
	aAnno := parseAWSAnnotations(config.Annotations)

	// Does this VM get an associatable elastic IP?
	if aAnno.elasticIP != "" {
		err := attachElasticIP(&name, &aAnno.elasticIP)
		if err != nil {
			awsErr := err.(awserr.Error)
			glog.Warningf("CreatePodSandbox: attaching elastic ip failed: %v, code = %v, msg = %v", err.Error(), awsErr.Code(), awsErr.Message())
		}
	}
}

func (p *podData) detach(vol string, force bool) error {
	glog.Infof("detach: enter: vol = %v", vol)

	req := &ec2.DetachVolumeInput{
		InstanceId: p.instanceId,
		VolumeId:   &vol,
		Force:      &force,
	}

	_, err := client.DetachVolume(req)
	if err != nil {
		return fmt.Errorf("detach: %v", err)
	}

	return nil
}

func (p *podData) Attach(vol, device string) (string, error) {
	glog.Infof("Attach: enter: vol = %v, device = %v", vol, device)
	p.lock.Lock()
	defer p.lock.Unlock()

	if dev, ok := p.attached[vol]; ok {
		if device != "" && dev == device {
			return dev, nil
		} else {
			return "", fmt.Errorf("Attach: tried to attach %v to %v but already attached to %v", vol, device, dev)
		}
	}

	dev := ""
	if device != "" {
		c := string(device[len(device)-1])
		if p.usedDevices[c] {
			return "", fmt.Errorf("Attach: tried to attach %v to %v, but already used", vol, device)
		}
		p.usedDevices[c] = true
		dev = device
	}

	if dev == "" {
		for i := 'f'; i <= 'p'; i++ {
			s := string(i)
			if !p.usedDevices[s] {
				p.usedDevices[s] = true
				dev = "/dev/xvd" + s
				break
			}
		}

		if dev == "" {
			glog.Errorf("Attach: Couldn't find a free device")
			return "", errors.New("Attach: Couldn't find a free device")
		}
	}

	glog.Infof("Attaching to %v", dev)
	req := &ec2.AttachVolumeInput{
		InstanceId: p.instanceId,
		VolumeId:   &vol,
		Device:     &dev,
	}
	attachResp, err := client.AttachVolume(req)

	if err != nil {
		glog.Errorf("Attach: AttachVolume failed: %v", err)
		return "", fmt.Errorf("Attach: attach failed: %v", err)
	}

	glog.Infof("Attach: AttachVolume succeeded")

	p.attached[vol] = dev

	for i := 1; i <= 10; i++ {
		glog.Infof("Attach: describing volume")
		req := &ec2.DescribeVolumesInput{
			VolumeIds: []*string{attachResp.VolumeId},
		}
		descResp, err := client.DescribeVolumes(req)
		if err != nil {
			glog.Errorf("Attach: DescribeVolumes failed: %v", err)
			return "", fmt.Errorf("Attach: describe failed: %v", err)
		}
		if len(descResp.Volumes) != 1 {
			glog.Errorf("Attach: DescribeVolumes didn't return one volume: %+v", descResp)
			return "", fmt.Errorf("Attach: describe didn't return any volumes")
		}
		if len(descResp.Volumes[0].Attachments) == 1 {
			if "attached" == *descResp.Volumes[0].Attachments[0].State {
				glog.Infof("Attach: success")
				return dev, nil
			}
		}

		glog.Infof("Attach (aws): descResp = %+v", descResp)
		time.Sleep(time.Duration(i) * time.Second)
	}

	glog.Errorf("Attach: describe never showed as attached")
	return "", fmt.Errorf("Attach: describe never showed as attached")
}

func (p *podData) NeedMount(vol string) bool {
	p.lock.Lock()
	defer p.lock.Unlock()

	_, ok := p.attached[vol]

	return !ok
}
