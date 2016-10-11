package virtualbox

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"time"

	"github.com/sjpotter/infranetes/pkg/infranetes/provider"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"

	lvm "github.com/apcera/libretto/virtualmachine"
	"github.com/apcera/libretto/virtualmachine/virtualbox"
	"github.com/sjpotter/infranetes/pkg/infranetes/provider/common"
)

type podData struct {
	vm          virtualbox.VM
	metadata    *kubeapi.PodSandboxMetadata
	annotations map[string]string
	createdAt   int64
	ip          string
	client      *common.Client
}

type vboxProvider struct {
	netDevice string
	vmSrc     string
	vmMap     map[string]*podData
}

func init() {
	provider.PodProviders.RegisterProvider("virtualbox", NewVBoxProvider)
}

type vboxConfig struct {
	NetDevice string
	VMSrc     string
}

func NewVBoxProvider() (provider.PodProvider, error) {
	var conf vboxConfig

	file, err := ioutil.ReadFile("virtualbox.json")
	if err != nil {
		return nil, fmt.Errorf("File error: %v\n", err)
	}

	json.Unmarshal(file, &conf)

	return &vboxProvider{
		netDevice: conf.NetDevice,
		vmSrc:     conf.VMSrc,
		vmMap:     make(map[string]*podData),
	}, nil
}

func (v *vboxProvider) getPodData(id string) (*podData, error) {
	podData, ok := v.vmMap[id]
	if !ok {
		return nil, fmt.Errorf("Invalid PodSandboxId (%v)", id)
	}
	return podData, nil
}

func (v *vboxProvider) RunPodSandbox(req *kubeapi.RunPodSandboxRequest) (*kubeapi.RunPodSandboxResponse, error) {
	config := virtualbox.Config{
		NICs: []virtualbox.NIC{
			{Idx: 1, Backing: virtualbox.Bridged, BackingDevice: v.netDevice},
		},
	}

	vm := virtualbox.VM{Src: v.vmSrc,
		Config: config,
	}

	if err := vm.Provision(); err != nil {
		return nil, fmt.Errorf("Failed to Provision: %v", err)
	}

	ips, err := vm.GetIPs()
	if err != nil {
		return nil, fmt.Errorf("CreatePodSandbox: error in GetIPs(): %v", err)
	}

	ip := ips[0].String()

	client, err := common.CreateClient(ip)
	if err != nil {
		return nil, fmt.Errorf("CreatePodSandbox: error in createClient(): %v", err)
	}

	name := vm.GetName()

	v.vmMap[name] = &podData{
		vm:          vm,
		metadata:    req.Config.Metadata,
		annotations: req.Config.Annotations,
		createdAt:   time.Now().Unix(),
		ip:          ips[0].String(),
		client:      client,
	}

	resp := &kubeapi.RunPodSandboxResponse{
		PodSandboxId: &name,
	}

	return resp, nil
}

func (v *vboxProvider) StopPodSandbox(req *kubeapi.StopPodSandboxRequest) (*kubeapi.StopPodSandboxResponse, error) {
	podData, err := v.getPodData(req.GetPodSandboxId())
	if err != nil {
		return nil, fmt.Errorf("StopPodSandbox: %v", err)
	}

	if err := podData.vm.Halt(); err != nil {
		return nil, fmt.Errorf("StopPodSandbox: %v", err)
	}

	resp := &kubeapi.StopPodSandboxResponse{}
	return resp, nil
}

func (v *vboxProvider) RemovePodSandbox(req *kubeapi.RemovePodSandboxRequest) (*kubeapi.RemovePodSandboxResponse, error) {
	podData, err := v.getPodData(req.GetPodSandboxId())
	if err != nil {
		return nil, fmt.Errorf("RemovePodSandbox: %v", err)
	}

	if err := podData.vm.Destroy(); err != nil {
		return nil, fmt.Errorf("RemovePodSandbox: %v", err)
	}

	delete(v.vmMap, req.GetPodSandboxId())

	resp := &kubeapi.RemovePodSandboxResponse{}
	return resp, nil
}

func (v *vboxProvider) PodSandboxStatus(req *kubeapi.PodSandboxStatusRequest) (*kubeapi.PodSandboxStatusResponse, error) {
	podData, err := v.getPodData(req.GetPodSandboxId())
	if err != nil {
		return nil, fmt.Errorf("PodSandboxStatus: %v", err)
	}

	network := &kubeapi.PodSandboxNetworkStatus{
		Ip: &podData.ip,
	}

	vmState, err := podData.vm.GetState()
	if err != nil {
		return nil, fmt.Errorf("PodSandboxStatus: error in GetState(): %v", err)
	}

	state := kubeapi.PodSandBoxState_READY
	if vmState != lvm.VMRunning {
		state = kubeapi.PodSandBoxState_NOTREADY
	}

	id := req.GetPodSandboxId()

	status := &kubeapi.PodSandboxStatus{
		Id:        &id,
		CreatedAt: &podData.createdAt,
		Metadata:  podData.metadata,
		Network:   network,
		State:     &state,
	}

	resp := &kubeapi.PodSandboxStatusResponse{
		Status: status,
	}

	return resp, nil
}

func (v *vboxProvider) ListPodSandbox(req *kubeapi.ListPodSandboxRequest) (*kubeapi.ListPodSandboxResponse, error) {
	sandboxes := []*kubeapi.PodSandbox{}

	for id, vm := range v.vmMap {
		sandbox := &kubeapi.PodSandbox{
			CreatedAt: &vm.createdAt,
			Id:        &id,
			Metadata:  vm.metadata,
		}
		state := kubeapi.PodSandBoxState_READY
		sandbox.State = &state
		sandboxes = append(sandboxes, sandbox)
	}

	resp := &kubeapi.ListPodSandboxResponse{
		Items: sandboxes,
	}

	return resp, nil
}

func (v *vboxProvider) GetIP(podName string) (string, error) {
	if podData, ok := v.vmMap[podName]; ok == true {
		return podData.ip, nil
	}

	return "", fmt.Errorf("%v unknown pod name", podName)
}

func (v *vboxProvider) GetClient(podName string) (*common.Client, error) {
	if podData, ok := v.vmMap[podName]; ok == true {
		return podData.client, nil
	}

	return nil, fmt.Errorf("%v unknown pod name", podName)
}

func (v *vboxProvider) GetVMList() []string {
	ret := []string{}
	for name := range v.vmMap {
		ret = append(ret, name)
	}

	return ret
}
