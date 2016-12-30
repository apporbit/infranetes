package gcp

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"strconv"

	"github.com/golang/glog"

	gcpvm "github.com/apcera/libretto/virtualmachine/gcp"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"

	"github.com/sjpotter/infranetes/cmd/infranetes/flags"
	"github.com/sjpotter/infranetes/pkg/infranetes/provider"
	"github.com/sjpotter/infranetes/pkg/infranetes/provider/common"
	"github.com/sjpotter/infranetes/pkg/utils"
)

func init() {
	provider.PodProviders.RegisterProvider("gcp", NewGCPPodProvider)
}

type gcpPodProvider struct {
	config *gceConfig
	ipList *utils.Deque
}

type gcePodData struct {
	ip string
}

type gceConfig struct {
	Zone        string
	SourceImage string
	Project     string
	Scope       string
	AuthFile    string
}

func NewGCPPodProvider() (provider.PodProvider, error) {
	var conf gceConfig

	file, err := ioutil.ReadFile("gce.json")
	if err != nil {
		return nil, fmt.Errorf("File error: %v\n", err)
	}

	json.Unmarshal(file, &conf)

	if conf.SourceImage == "" || conf.Zone == "" || conf.Project == "" || conf.Scope == "" || conf.AuthFile == "" {
		msg := fmt.Sprintf("Failed to read in complete config file: conf = %+v", conf)
		glog.Info(msg)
		return nil, fmt.Errorf(msg)
	}

	ipList := utils.NewDeque()
	for i := 1; i <= 255; i++ {
		ipList.Append(fmt.Sprint(*flags.IPBase + "." + strconv.Itoa(i)))
	}

	return &gcpPodProvider{
		ipList: ipList,
	}, nil
}

func (*gcpPodProvider) UpdatePodState(data *common.PodData) {
	if data.Booted {
		data.UpdatePodState()
	}
}

func (p *gcpPodProvider) bootSandbox(vm *gcpvm.VM, config *kubeapi.PodSandboxConfig, name string) (*common.PodData, error) {
	startProxy, createInterface, setHostname, handleRoutes := common.ParseAnnotations(config.Annotations)

	if err := vm.Provision(); err != nil {
		return nil, fmt.Errorf("failed to provision vm: %v\n", err)
	}

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

	client, err := common.CreateRealClient(ip)
	if err != nil {
		return nil, fmt.Errorf("CreatePodSandbox: error in createClient(): %v", err)
	}

	if startProxy {
		err = client.StartProxy()
		if err != nil {
			client.Close()
			glog.Warningf("CreatePodSandbox: Couldn't start kube-proxy: %v", err)
		}
	}

	if setHostname {
		err = client.SetHostname(config.GetHostname())
		if err != nil {
			glog.Warningf("CreatePodSandbox: couldn't set hostname to %v: %v", config.GetHostname(), err)
		}
	}

	podIp := ip
	if createInterface {
		podIp = name
	}
	err = client.SetPodIP(podIp, createInterface)

	if err != nil {
		glog.Warningf("CreatePodSandbox: Failed to configure inteface: %v", err)
	}

	err = client.SetSandboxConfig(config)
	if err != nil {
		glog.Warningf("CreatePodSandbox: Failed to save sandbox config: %v", err)
	}

	if handleRoutes {
		err = addRoute(vm, podIp)
		if err != nil {
			glog.Warningf("addRoute failed: %v", err)
		}
	}

	booted := true

	locaData := &gcePodData{
		ip: podIp,
	}

	podData := common.NewPodData(vm, &name, config.Metadata, config.Annotations, config.Labels, podIp, config.Linux, client, booted, &locaData)

	return podData, nil
}

func (v *gcpPodProvider) RunPodSandbox(req *kubeapi.RunPodSandboxRequest) (*common.PodData, error) {
	name := "infranetes-" + req.GetConfig().GetMetadata().GetUid()
	disk := []gcpvm.Disk{{DiskType: "pd-standard", DiskSizeGb: 10, AutoDelete: true}}

	vm := &gcpvm.VM{
		//Scopes:        []string{"https://www.googleapis.com/auth/cloud-platform"},
		//AccountFile: "/root/gcp.json",
		Name:          name,
		Zone:          v.config.Zone,
		MachineType:   "g1-small",
		SourceImage:   v.config.SourceImage,
		Disks:         disk,
		Preemptible:   false,
		Network:       "default",
		Subnetwork:    "default",
		UseInternalIP: false,
		ImageProjects: []string{"engineering-lab"},
		Project:       "engineering-lab",
		Scopes:        []string{v.config.Scope},
		AccountFile:   v.config.AuthFile,
		Tags:          []string{"infranetes:true"},
	}

	podIp := v.ipList.Shift().(string)

	return v.bootSandbox(vm, req.Config, podIp)
}

func (v *gcpPodProvider) PreCreateContainer(data *common.PodData, req *kubeapi.CreateContainerRequest, imageStatus func(req *kubeapi.ImageStatusRequest) (*kubeapi.ImageStatusResponse, error)) error {
	//FIXME: will when image support is added
	return nil
}

func (v *gcpPodProvider) StopPodSandbox(podData *common.PodData) {}

func (v *gcpPodProvider) RemovePodSandbox(data *common.PodData) {
	ip := data.ProviderData.(*gcePodData).ip
	vm := data.VM.(*gcpvm.VM)

	glog.Infof("RemovePodSandbox: release IP: %v", ip)

	err := delRoute(vm)
	if err != nil {
		glog.Warningf("del route failed: %v", err)
		return
	}

	v.ipList.Append(ip)
}

func (v *gcpPodProvider) PodSandboxStatus(podData *common.PodData) {}

func (v *gcpPodProvider) ListInstances() ([]*common.PodData, error) {
	//FIXME: Implement - Needs tagging
	return nil, nil
}
