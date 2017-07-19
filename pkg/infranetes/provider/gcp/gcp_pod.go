package gcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"strconv"
	"sync"

	"github.com/golang/glog"

	gcpvm "github.com/apcera/libretto/virtualmachine/gcp"

	"github.com/apporbit/infranetes/cmd/infranetes/flags"
	"github.com/apporbit/infranetes/pkg/common/gcp"
	"github.com/apporbit/infranetes/pkg/infranetes/provider"
	"github.com/apporbit/infranetes/pkg/infranetes/provider/common"
	"github.com/apporbit/infranetes/pkg/infranetes/types"
	"github.com/apporbit/infranetes/pkg/utils"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1/runtime"
)

const (
	devPrefix = "/dev/disk/by-id/google-"
)

func init() {
	provider.PodProviders.RegisterProvider("gcp", NewGCPPodProvider)
}

type gcpPodProvider struct {
	config   *gcp.GceConfig
	ipList   *utils.Deque
	imagePod bool
}

type podData struct {
	lock       sync.Mutex
	instanceId *string
	volumes    []*types.Volume
	attached   map[string]string
	service    *gcp.GcpSvcWrapper
}

func NewGCPPodProvider() (provider.PodProvider, error) {
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

	// FIXME: add autodetection like AWS
	if *flags.MasterIP == "" || *flags.IPBase == "" {
		return nil, fmt.Errorf("GCP doesn't have autodetection yet: MasterIP = %v, IPBase = %v", *flags.MasterIP, *flags.IPBase)
	}

	ipList := utils.NewDeque()
	for i := 2; i <= 254; i++ {
		ipList.Append(fmt.Sprint(*flags.IPBase + "." + strconv.Itoa(i)))
	}

	return &gcpPodProvider{
		config: &conf,
		ipList: ipList,
	}, nil
}

func (*gcpPodProvider) UpdatePodState(data *common.PodData) {
	if data.Booted {
		data.UpdatePodState()
	}
}

func (p *gcpPodProvider) tagImage(name string) {
	s, err := gcp.GetService(p.config.AuthFile, p.config.Project, p.config.Zone, []string{p.config.Scope})
	if err != nil {
		glog.Errorf("tagImage: failed to tag: %v", name)
		return
	}
	err = s.TagNewInstance(name)
	if err != nil {
		glog.Errorf("tagImage: failed: %v", err)
	}
}

func (p *gcpPodProvider) bootSandbox(vm *gcpvm.VM, config *kubeapi.PodSandboxConfig, name string, volumes []*types.Volume) (*common.PodData, error) {
	cAnno := common.ParseCommonAnnotations(config.Annotations)

	s, err := gcp.GetService(p.config.AuthFile, p.config.Project, p.config.Zone, []string{p.config.Scope})
	if err != nil {
		return nil, fmt.Errorf("CreatePodSandbox: failed to get gcp service")
	}

	// Testing
	attached := make(map[string]string)
	for _, v := range volumes {
		vm.Disks = append(vm.Disks, gcpvm.Disk{AutoDelete: false, Name: v.Volume})
		attached[v.Volume] = devPrefix + v.Volume
	}

	if err := vm.Provision(); err != nil {
		return nil, fmt.Errorf("CreatePodSandbox: failed to provision vm: %v\n", err)
	}

	ips, err := vm.GetIPs()
	if err != nil {
		return nil, fmt.Errorf("CreatePodSandbox: error in GetIPs(): %v", err)
	}

	p.tagImage(vm.Name)

	glog.Infof("CreatePodSandbox: ips = %v", ips)

	// FIXME: Perhaps better way to choose public vs private ip
	index := 1
	podIp := ips[index].String()

	client, err := common.CreateRealClient(podIp)
	if err != nil {
		return nil, fmt.Errorf("CreatePodSandbox: error in createClient(): %v", err)
	}

	providerData := &podData{
		instanceId: &vm.Name,
		volumes:    volumes,
		attached:   attached, // attached:   make(map[string]string),
		service:    s,
	}

	err = client.SetSandboxConfig(config)
	if err != nil {
		glog.Warningf("CreatePodSandbox: Failed to save sandbox config: %v", err)
	}

	// Testing
	for _, vol := range volumes {
		if vol.MountPoint != "" {
			device := providerData.attached[vol.Volume]
			err := client.MountFs(device, vol.MountPoint, vol.FsType, vol.ReadOnly)
			if err != nil {
				glog.Warningf("bootSandbox: failed to mount %v(%v) on %v in %v", vol.Volume, device, vol.MountPoint, vm.Name)
			}
		}
	}

	err = client.SetPodIP(podIp)
	if err != nil {
		glog.Warningf("CreatePodSandbox: Failed to configure inteface: %v", err)
	}

	if cAnno.StartProxy {
		err = client.StartProxy()
		if err != nil {
			client.Close()
			glog.Warningf("CreatePodSandbox: Couldn't start kube-proxy: %v", err)
		}
	}

	if cAnno.SetHostname {
		err = client.SetHostname(config.GetHostname())
		if err != nil {
			glog.Warningf("CreatePodSandbox: couldn't set hostname to %v: %v", config.GetHostname(), err)
		}
	}

	booted := true

	podData := common.NewPodData(vm, name, config.Metadata, config.Annotations, config.Labels, podIp, config.Linux, client, booted, providerData)

	return podData, nil
}

func (v *gcpPodProvider) RunPodSandbox(req *kubeapi.RunPodSandboxRequest, volumes []*types.Volume) (*common.PodData, error) {
	name := "infranetes-" + req.GetConfig().GetMetadata().GetUid()
	podIp := v.ipList.Shift().(string)

	disk := []gcpvm.Disk{{DiskType: "pd-standard", DiskSizeGb: 10, AutoDelete: true}}

	vm := &gcpvm.VM{
		Name:             name,
		Zone:             v.config.Zone,
		MachineType:      "g1-small",
		SourceImage:      v.config.SourceImage,
		Disks:            disk,
		Preemptible:      false,
		Network:          v.config.Network,
		Subnetwork:       v.config.Subnet,
		UseInternalIP:    false,
		ImageProjects:    []string{v.config.Project},
		Project:          v.config.Project,
		Scopes:           []string{v.config.Scope},
		AccountFile:      v.config.AuthFile,
		Tags:             []string{"infranetes"},
		PrivateIPAddress: podIp,
	}

	if !v.imagePod { // Traditional Pod, but within a VM
		ret, err := v.bootSandbox(vm, req.Config, podIp, volumes)
		if err == nil {
			// FIXME: Google's version of elastic IP handling goes here
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
func (v *gcpPodProvider) PreCreateContainer(data *common.PodData, req *kubeapi.CreateContainerRequest, imageStatus func(req *kubeapi.ImageStatusRequest) (*kubeapi.ImageStatusResponse, error)) error {
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
	vm, ok := data.VM.(*gcpvm.VM)
	if !ok {
		return errors.New("PreCreateContainer: podData's VM wasn't an aws VM struct")
	}

	result, err := imageStatus(&kubeapi.ImageStatusRequest{Image: req.Config.Image})
	if err == nil && result.Image != nil {
		glog.Infof("PreCreateContainer: translated %v to %v", req.Config.Image.Image, result.Image.Id)
		vm.SourceImage = result.Image.Id
	} else {
		return fmt.Errorf("PreCreateContainer: Couldn't translate %v: err = %v and result = %v", req.Config.Image.Image, err, result)
	}

	newPodData, err := v.bootSandbox(vm, req.SandboxConfig, data.Ip, volumes)
	if err != nil {
		return fmt.Errorf("PreCreateContainer: couldn't boot VM: %v", err)
	}

	// FIXME: Google's version of elastic IP handling goes here
	//handleElasticIP(req.GetSandboxConfig(), vm.GetName())

	data.Booted = true

	data.Client = newPodData.Client
	data.ProviderData = newPodData.ProviderData

	return nil
}
func (v *gcpPodProvider) StopPodSandbox(pdata *common.PodData) {
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

func (v *gcpPodProvider) RemovePodSandbox(data *common.PodData) {
	glog.Infof("RemovePodSandbox: release IP: %v", data.Ip)

	v.ipList.Append(data.Ip)
}

func (v *gcpPodProvider) PodSandboxStatus(podData *common.PodData) {}

func (v *gcpPodProvider) ListInstances() ([]*common.PodData, error) {
	glog.Infof("ListInstances: enter")
	s, err := gcp.GetService(v.config.AuthFile, v.config.Project, v.config.Zone, []string{v.config.Scope})
	if err != nil {
		return nil, fmt.Errorf("ListInstances: GetServices failed: %v", err)
	}

	instances, err := s.ListInstances()
	if err != nil {
		return nil, err
	}

	podDatas := []*common.PodData{}
	for _, instance := range instances {
		podIp := instance.NetworkInterfaces[0].NetworkIP

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

		vm := &gcpvm.VM{
			Name:        instance.Name,
			Zone:        v.config.Zone,
			Project:     v.config.Project,
			Scopes:      []string{v.config.Scope},
			AccountFile: v.config.AuthFile,
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

	device = devPrefix + vol

	glog.Infof("Attaching to %v", device)
	err := p.service.AttachDisk(vol, *p.instanceId, vol)
	glog.Infof("Attach: AttachVolume succeeded")

	p.attached[vol] = device

	return device, err
}

func (p *podData) detach(vol string, force bool) error {
	glog.Infof("detach: enter: vol = %v", vol)

	device := devPrefix + vol
	err := p.service.DetatchDisk(vol, device)

	return err
}

func (p *podData) NeedMount(vol string) bool {
	p.lock.Lock()
	defer p.lock.Unlock()

	_, ok := p.attached[vol]

	return !ok
}
