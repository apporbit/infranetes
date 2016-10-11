package aws

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/golang/glog"

	"github.com/apcera/libretto/ssh"
	lvm "github.com/apcera/libretto/virtualmachine"
	"github.com/apcera/libretto/virtualmachine/aws"

	"github.com/sjpotter/infranetes/pkg/infranetes/provider"
	"github.com/sjpotter/infranetes/pkg/infranetes/provider/common"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

type podData struct {
	vm                 *aws.VM
	metadata           *kubeapi.PodSandboxMetadata
	annotations        map[string]string
	createdAt          int64
	ip                 string
	labels             map[string]string
	linux              *kubeapi.LinuxPodSandboxConfig
	stateLock          sync.Mutex
	client             *common.Client
	podState           kubeapi.PodSandBoxState
	vmState            string
	vmStateLastChecked time.Time
}

type awsProvider struct {
	vmMap     map[string]*podData
	vmMapLock sync.Mutex
	config    *awsConfig
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
		vmMap:  make(map[string]*podData),
		config: &conf,
	}, nil
}

func (v *awsProvider) getPodData(id string) (*podData, error) {
	v.vmMapLock.Lock()
	defer v.vmMapLock.Unlock()

	podData, ok := v.vmMap[id]
	if !ok {
		return nil, fmt.Errorf("Invalid PodSandboxId (%v)", id)
	}
	return podData, nil
}

func (v *awsProvider) RunPodSandbox(req *kubeapi.RunPodSandboxRequest) (*kubeapi.RunPodSandboxResponse, error) {
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
	if client == nil {
		glog.Infof("WARNING WARNING WARNING: returned a nil GRPC client")
		glog.Infof("WARNING WARNING WARNING: returned a nil GRPC client")
		glog.Infof("WARNING WARNING WARNING: returned a nil GRPC client")
		vm.Destroy()
		return nil, fmt.Errorf("returned a nil GRPC client")
	}

	v.vmMapLock.Lock()

	v.vmMap[name] = &podData{
		vm:                 vm,
		metadata:           req.Config.Metadata,
		annotations:        req.Config.Annotations,
		createdAt:          time.Now().Unix(),
		ip:                 ip,
		labels:             req.Config.Labels,
		linux:              req.Config.Linux,
		client:             client,
		podState:           kubeapi.PodSandBoxState_READY,
		vmState:            lvm.VMRunning,
		vmStateLastChecked: time.Now(),
	}

	v.vmMapLock.Unlock()

	resp := &kubeapi.RunPodSandboxResponse{
		PodSandboxId: &name,
	}

	return resp, nil
}

func (v *awsProvider) StopPodSandbox(req *kubeapi.StopPodSandboxRequest) (*kubeapi.StopPodSandboxResponse, error) {
	podData, err := v.getPodData(req.GetPodSandboxId())
	if err != nil {
		return nil, fmt.Errorf("StopPodSandbox: %v", err)
	}

	podData.stateLock.Lock()
	podData.podState = kubeapi.PodSandBoxState_NOTREADY
	podData.stateLock.Unlock()

	resp := &kubeapi.StopPodSandboxResponse{}
	return resp, nil
}

func (v *awsProvider) RemovePodSandbox(req *kubeapi.RemovePodSandboxRequest) (*kubeapi.RemovePodSandboxResponse, error) {
	podData, err := v.getPodData(req.GetPodSandboxId())
	if err != nil {
		return nil, fmt.Errorf("RemovePodSandbox: %v", err)
	}

	if err := podData.vm.Destroy(); err != nil {
		return nil, fmt.Errorf("RemovePodSandbox: %v", err)
	}

	podData.client.Close()

	v.vmMapLock.Lock()
	delete(v.vmMap, req.GetPodSandboxId())
	v.vmMapLock.Unlock()

	resp := &kubeapi.RemovePodSandboxResponse{}
	return resp, nil
}

func updateVMState(podData *podData) string {
	podData.stateLock.Lock()

	ret := podData.vmState

	if time.Now().After(podData.vmStateLastChecked.Add(30 * time.Second)) {
		vmState, err := podData.vm.GetState()
		if err == nil {
			podData.vmState = vmState
			podData.vmStateLastChecked = time.Now()
			ret = podData.vmState
		}
	}

	podData.stateLock.Unlock()

	return ret
}

func (v *awsProvider) PodSandboxStatus(req *kubeapi.PodSandboxStatusRequest) (*kubeapi.PodSandboxStatusResponse, error) {
	podData, err := v.getPodData(req.GetPodSandboxId())
	if err != nil {
		return nil, fmt.Errorf("PodSandboxStatus: %v", err)
	}

	network := &kubeapi.PodSandboxNetworkStatus{
		Ip: &podData.ip,
	}

	vmState := updateVMState(podData)

	state := podData.podState
	if vmState != lvm.VMRunning {
		state = kubeapi.PodSandBoxState_NOTREADY
	}

	id := req.GetPodSandboxId()

	net := "host"
	linux := &kubeapi.LinuxPodSandboxStatus{
		Namespaces: &kubeapi.Namespace{
			Network: &net,
			Options: podData.linux.NamespaceOptions,
		},
	}

	status := &kubeapi.PodSandboxStatus{
		Id:          &id,
		CreatedAt:   &podData.createdAt,
		Metadata:    podData.metadata,
		Network:     network,
		State:       &state,
		Linux:       linux,
		Labels:      podData.labels,
		Annotations: podData.annotations,
	}

	resp := &kubeapi.PodSandboxStatusResponse{
		Status: status,
	}

	return resp, nil
}

func (v *awsProvider) ListPodSandbox(req *kubeapi.ListPodSandboxRequest) (*kubeapi.ListPodSandboxResponse, error) {
	sandboxes := []*kubeapi.PodSandbox{}

	v.vmMapLock.Lock()
	defer v.vmMapLock.Unlock()

	glog.V(1).Infof("ListPodSandbox: len of vmMap = %v", len(v.vmMap))

	for id, podData := range v.vmMap {
		glog.V(1).Infof("ListPodSandbox:v podData for %v = %+v", id, podData)
		vmState := updateVMState(podData)

		if vmState == "terminated" || vmState == "shutting-down" {
			glog.V(1).Infof("ListPodSandbox: filtering out %v because the vm is no longer available", id)
			continue
		}

		podState := podData.podState
		if vmState != lvm.VMRunning {
			podState = kubeapi.PodSandBoxState_NOTREADY
		}

		if req.Filter != nil {
			if req.Filter.GetId() != "" && req.Filter.GetId() != id {
				glog.V(1).Infof("ListPodSandbox: filtering out %v because doesn't match %v", id, req.Filter.GetId())
				continue
			}

			if req.Filter.GetState() != podState {
				glog.V(1).Infof("ListPodSandbox: filtering out %v because want %v and got %v", id, req.Filter.GetState(), podState)
				continue
			}

			filterLabels := req.Filter.GetLabelSelector()

			if filter, msg := filterByLabels(filterLabels, podData.labels); filter {
				glog.V(1).Infof("ListPodSandbox: filtering out %v on labels as %v", id, msg)
				continue
			}
		}

		sandbox := &kubeapi.PodSandbox{
			CreatedAt:   &podData.createdAt,
			Id:          &id,
			Metadata:    podData.metadata,
			Labels:      podData.labels,
			Annotations: podData.annotations,
			State:       &podState,
		}

		glog.V(1).Infof("ListPodSandbox Appending a sandbox for %v to sandboxes", id)

		sandboxes = append(sandboxes, sandbox)
	}

	glog.V(1).Infof("ListPodSandbox: len of sandboxes returning = %v", len(sandboxes))

	resp := &kubeapi.ListPodSandboxResponse{
		Items: sandboxes,
	}

	return resp, nil
}

func filterByLabels(filterLabels map[string]string, podLabels map[string]string) (bool, string) {
	for key, filterVal := range filterLabels {
		if podVal, ok := podLabels[key]; !ok {
			return true, fmt.Sprintf("didn't find key %v in local labels: %+v", key, podLabels)
		} else {
			if podVal != filterVal {
				return true, fmt.Sprintf("key value's didn't match %v and %v", filterVal, podVal)
			}
		}
	}

	return false, ""
}

func (v *awsProvider) GetIP(podName string) (string, error) {
	podData, err := v.getPodData(podName)

	if err != nil {
		return "", fmt.Errorf("%v unknown pod name", podName)
	}

	return podData.ip, nil
}

func (v *awsProvider) GetClient(podName string) (*common.Client, error) {
	podData, err := v.getPodData(podName)

	if err != nil {
		return nil, fmt.Errorf("%v unknown pod name", podName)
	}

	return podData.client, nil
}

func (v *awsProvider) GetVMList() []string {
	ret := []string{}
	v.vmMapLock.Lock()
	for name := range v.vmMap {
		ret = append(ret, name)
	}
	v.vmMapLock.Unlock()

	return ret
}
