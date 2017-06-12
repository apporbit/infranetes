package vsphere

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"strings"

	"github.com/golang/glog"

	"github.com/apcera/libretto/ssh"
	vsvm "github.com/apcera/libretto/virtualmachine/vsphere"

	"github.com/apporbit/infranetes/pkg/infranetes/provider"
	"github.com/apporbit/infranetes/pkg/infranetes/provider/common"
	"github.com/apporbit/infranetes/pkg/infranetes/types"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1"
)

type podData struct{}

type vspherePodProvider struct {
	config *vsphereConfig
}

func init() {
	provider.PodProviders.RegisterProvider("vsphere", NewAWSPodProvider)
}

func NewAWSPodProvider() (provider.PodProvider, error) {
	var conf vsphereConfig

	file, err := ioutil.ReadFile("vsphere.json")
	if err != nil {
		return nil, fmt.Errorf("File error: %v\n", err)
	}

	json.Unmarshal(file, &conf)

	if conf.Template == "" || conf.Datacenter == "" || conf.Datastore == "" || conf.Host == "" || conf.Location == "" || conf.Network == "" || conf.Username == "" || conf.Password == "" {
		msg := fmt.Sprintf("Failed to read in complete config file: conf = %+v", conf)
		glog.Info(msg)
		return nil, fmt.Errorf(msg)
	}

	glog.Infof("Validating Vsphere Credentials")
	err = verifyCreds(conf.Host, conf.Username, conf.Password, conf.Insecure)
	if err != nil {
		msg := fmt.Sprintf("Failed to Validated Vsphere Credentials: %v", err)
		glog.Info(msg)
		return nil, fmt.Errorf(msg)
	}
	glog.Infof("Validated Credentials")

	return &vspherePodProvider{
		config: &conf,
	}, nil
}

func (*vspherePodProvider) UpdatePodState(data *common.PodData) {
	if data.Booted {
		data.UpdatePodState()
	}
}

func (p *vspherePodProvider) bootSandbox(vm *vsvm.VM, config *kubeapi.PodSandboxConfig, name string) (*common.PodData, error) {
	// 1. Parse Annotations from PodSandboxConfig
	cAnno := common.ParseCommonAnnotations(config.Annotations)

	// 2. Boot VM
	if err := vm.Provision(); err != nil {
		return nil, fmt.Errorf("failed to provision vm: %v\n", err)
	}

	// 3. Extract IP Info
	ips, err := vm.GetIPs()
	if err != nil {
		return nil, fmt.Errorf("CreatePodSandbox: error in GetIPs(): %v", err)
	}

	glog.Infof("CreatePodSandbox: ips = %v", ips)

	// FIXME: Need to learn from practic which is the correct index
	index := 0
	podIp := ips[index].String()

	glog.Infof("CreatePodSandbox: podIp = %v", podIp)

	// 4. Connect to VMServer in VM
	client, err := common.CreateRealClient(podIp)
	if err != nil {
		return nil, fmt.Errorf("CreatePodSandbox: error in createClient(): %v", err)
	}

	// 5. Setup Instance / VM Correctly
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

	for _, r := range p.config.Routes {
		glog.Infof("AddRoute: %+v", r)
		_, err := client.AddRoute(&r)
		if err != nil {
			glog.Warningf("CreatePodSandbox: %v", err)
		}
	}

	providerData := &podData{}

	booted := true

	podData := common.NewPodData(vm, name, config.Metadata, config.Annotations, config.Labels, podIp, config.Linux, client, booted, providerData)

	return podData, nil
}

func (v *vspherePodProvider) RunPodSandbox(req *kubeapi.RunPodSandboxRequest, voluems []*types.Volume) (*common.PodData, error) {
	podIp := ""
	vm := v.createVM(req.Config, podIp)

	return v.bootSandbox(vm, req.Config, vm.Name)
}

func (v *vspherePodProvider) PreCreateContainer(data *common.PodData, req *kubeapi.CreateContainerRequest, imageStatus func(req *kubeapi.ImageStatusRequest) (*kubeapi.ImageStatusResponse, error)) error {
	//FIXME: image support to be added
	return nil
}

func (v *vspherePodProvider) StopPodSandbox(podData *common.PodData) {}

func (v *vspherePodProvider) RemovePodSandbox(data *common.PodData) {
	glog.Infof("RemovePodSandbox: release IP: %v", data.Ip)

	//v.ipList.Append(data.Ip)
}

func (v *vspherePodProvider) PodSandboxStatus(podData *common.PodData) {}

func (v *vspherePodProvider) ListInstances() ([]*common.PodData, error) {
	vms, err := listVMs(v.config.Host, v.config.Username, v.config.Password, v.config.Datacenter, v.config.Insecure)
	if err != nil {
		return nil, fmt.Errorf("ListInstances: %v", err)
	}

	podDatas := []*common.PodData{}
	for _, vm := range vms {
		podIp := vm.Guest.IpAddress

		if !strings.HasPrefix(vm.Name, "kube-infra-") {
			continue
		}

		client, err := common.CreateRealClient(podIp)

		podIp, err = client.GetPodIP()
		if err != nil {
			continue
		}

		config, err := client.GetSandboxConfig()
		if err != nil {
			continue
		}

		name := vm.Name

		vm := &vsvm.VM{
			Name:       vm.Name,
			Host:       v.config.Host,
			Username:   v.config.Username,
			Password:   v.config.Password,
			Datacenter: v.config.Datacenter,
			Datastores: []string{v.config.Datastore},
			Insecure:   v.config.Insecure,
		}

		providerData := &podData{}

		glog.Infof("ListInstances: creating a podData for %v", name)
		booted := true
		podData := common.NewPodData(vm, name, config.Metadata, config.Annotations, config.Labels, podIp, config.Linux, client, booted, providerData)

		podDatas = append(podDatas, podData)
	}

	return podDatas, nil
}

func (v *vspherePodProvider) createVM(config *kubeapi.PodSandboxConfig, podIp string) *vsvm.VM {
	//aAnno := parseAWSAnnotations(config.Annotations)

	vm := &vsvm.VM{
		Name:            "kube-infra-" + config.Metadata.Uid,
		Host:            v.config.Host,
		Username:        v.config.Username,
		Password:        v.config.Password,
		Datacenter:      v.config.Datacenter,
		Datastores:      []string{v.config.Datastore},
		Networks:        map[string]string{"nw1": v.config.Network},
		SkipExisting:    true,
		Insecure:        v.config.Insecure,
		Template:        v.config.Template,
		OvfPath:         "/dev/null",
		UseLinkedClones: true,

		Credentials: ssh.Credentials{
			SSHUser:     "ubuntu",
			SSHPassword: "ubuntu",
		},
		Destination: vsvm.Destination{
			DestinationType: vsvm.DestinationTypeHost,
			DestinationName: v.config.Location,
		},
	}

	// Fill in VM struct with data from annotations if required
	// overrideVMDefault(vm, aAnno)

	return vm
}

func (p *podData) Attach(vol, device string) (string, error) {
	return "", errors.New("Attach: Not implemented yet")
}

func (p *podData) NeedMount(vol string) bool {
	// FIXME: not implemented yet
	return false
}
