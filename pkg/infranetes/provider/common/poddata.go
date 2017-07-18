package common

import (
	"errors"
	"fmt"
	//	"runtime"
	"sync"
	"time"

	lvm "github.com/apcera/libretto/virtualmachine"
	"github.com/golang/glog"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1/runtime"
)

type ProviderData interface {
	Attach(volume, device string) (string, error)
	NeedMount(volume string) bool
}

type PodData struct {
	VM           lvm.VirtualMachine
	Id           string
	Metadata     *kubeapi.PodSandboxMetadata
	Annotations  map[string]string
	Labels       map[string]string
	CreatedAt    int64
	Ip           string
	Linux        *kubeapi.LinuxPodSandboxConfig
	stateLock    sync.RWMutex
	Client       Client
	PodState     kubeapi.PodSandboxState
	Booted       bool
	BootLock     sync.Mutex
	ProviderData ProviderData
	ContLogs     map[string]string
}

func NewPodData(vm lvm.VirtualMachine, id string, meta *kubeapi.PodSandboxMetadata, anno map[string]string,
	labels map[string]string, ip string, linux *kubeapi.LinuxPodSandboxConfig, client Client, booted bool,
	providerData ProviderData) *PodData {
	return &PodData{
		VM:           vm,
		Id:           id,
		Metadata:     meta,
		Annotations:  anno,
		Labels:       labels,
		CreatedAt:    time.Now().Unix(),
		Ip:           ip,
		Linux:        linux,
		Client:       client,
		PodState:     kubeapi.PodSandboxState_SANDBOX_READY,
		Booted:       booted,
		ProviderData: providerData,
		ContLogs:     make(map[string]string),
	}
}

func (p *PodData) Lock() {
	/*	if glog.V(10) {
			glog.Infof("podData.Lock(): pre state = %v", p.stateLock)
		}
		_, file, no, ok := runtime.Caller(1)
		if ok {
			if glog.V(10) {
				glog.Infof("podData.Lock() called from %s#%d\n", file, no)
			}
		} */

	p.stateLock.Lock()
	/*	if glog.V(10) {
		glog.Infof("podData.Lock(): post state = %v", p.stateLock)
	} */
}

func (p *PodData) Unlock() {
	/*	if glog.V(10) {
			glog.Infof("podData.Unlock(): pre state = %v", p.stateLock)
		}
		_, file, no, ok := runtime.Caller(1)
		if ok {
			if glog.V(10) {
				glog.Infof("podData.Unlock(): called from %s#%d\n", file, no)
			}
		} */
	p.stateLock.Unlock()

	/*	if glog.V(10) {
		glog.Infof("podData.Unlock(): post state = %v", p.stateLock)
	} */
}

func (p *PodData) RLock() {
	/*	if glog.V(10) {
			glog.Infof("podData.RLock(): pre state = %v", p.stateLock)
		}
		_, file, no, ok := runtime.Caller(1)
		if ok {
			if glog.V(10) {
				glog.Infof("podData.RLock() called from %s#%d\n", file, no)
			}
		} */

	p.stateLock.RLock()

	/*	if glog.V(10) {
		glog.Infof("podData.RLock(): post state = %v", p.stateLock)
	} */
}

func (p *PodData) RUnlock() {
	/*	if glog.V(10) {
			glog.Infof("podData.RUnlock(): pre state = %v", p.stateLock)
		}
		_, file, no, ok := runtime.Caller(1)
		if ok {
			if glog.V(10) {
				glog.Infof("podData.RUnlock() called from %s#%d\n", file, no)
			}
		} */

	p.stateLock.RUnlock()

	/*	if glog.V(10) {
		glog.Infof("podData.RUnlock(): post state = %v", p.stateLock)
	} */
}

/* Expect StateLock to already be taken */
func (p *PodData) StopPod() error {
	p.PodState = kubeapi.PodSandboxState_SANDBOX_NOTREADY

	return nil
}

func (p *PodData) RemovePod() error {
	p.Client.Close()
	p.Client = nil

	return nil
}

func (p *PodData) PodStatus() *kubeapi.PodSandboxStatus {
	network := &kubeapi.PodSandboxNetworkStatus{
		Ip: p.Ip,
	}

	linux := &kubeapi.LinuxPodSandboxStatus{
		Namespaces: &kubeapi.Namespace{
			Options: p.Linux.SecurityContext.NamespaceOptions,
		},
	}

	status := &kubeapi.PodSandboxStatus{
		Id:          p.Id,
		CreatedAt:   p.CreatedAt,
		Metadata:    p.Metadata,
		Network:     network,
		Linux:       linux,
		Labels:      p.Labels,
		Annotations: p.Annotations,
		State:       p.GetPodState(),
	}

	return status
}

func (p *PodData) Filter(filter *kubeapi.PodSandboxFilter) (bool, string) {
	if p.Client == nil {
		return true, fmt.Sprintf("no longer exists, client is nil")
	}

	if filter != nil {
		if filter.Id != "" && filter.GetId() != p.Id {
			return true, fmt.Sprintf("doesn't match %v", filter.GetId())
		}

		if filter.State != nil && filter.GetState().State != p.PodState {
			return true, fmt.Sprintf("want %v and got %v", filter.GetState(), p.PodState)
		}

		for key, filterVal := range filter.GetLabelSelector() {
			if podVal, ok := p.Labels[key]; !ok {
				return true, fmt.Sprintf("didn't find key %v in local labels: %+v", key, p.Labels)
			} else {
				if podVal != filterVal {
					return true, fmt.Sprintf("key value's didn't match %v and %v", filterVal, podVal)
				}
			}
		}
	}

	return false, ""
}

func (p *PodData) GetSandbox() *kubeapi.PodSandbox {
	state := p.GetPodState()

	return &kubeapi.PodSandbox{
		CreatedAt:   p.CreatedAt,
		Id:          p.Id,
		Metadata:    p.Metadata,
		Labels:      p.Labels,
		Annotations: p.Annotations,
		State:       state,
	}
}

func (p *PodData) GetPodState() kubeapi.PodSandboxState {
	//i.e. once not ready, always not ready
	if p.PodState == kubeapi.PodSandboxState_SANDBOX_NOTREADY {
		return kubeapi.PodSandboxState_SANDBOX_NOTREADY
	}

	if !p.Booted { // don't update state on a not booted VM
		return kubeapi.PodSandboxState_SANDBOX_READY
	}

	err := p.Client.Ready()
	if err != nil {
		glog.Infof("GetPodState: pod %v not Ready: %v", err)
		return kubeapi.PodSandboxState_SANDBOX_NOTREADY
	}

	return kubeapi.PodSandboxState_SANDBOX_READY
}

func (p *PodData) UpdatePodState() {
	p.PodState = p.GetPodState()
}

func (p *PodData) AddContLogPath(cont string, path string) {
	p.Lock()
	defer p.Unlock()

	p.ContLogs[cont] = path
}

func (p *PodData) GetContLogPath(cont string) (string, bool) {
	p.RLock()
	defer p.RUnlock()

	ret, ok := p.ContLogs[cont]

	return ret, ok
}

func (p *PodData) AttachVol(vol string) (string, error) {
	if p.ProviderData == nil {
		return "", errors.New("Attach: No Provider Data")
	}

	return p.ProviderData.Attach(vol, "")
}

func (p *PodData) NeedMount(vol string) bool {
	if p.ProviderData == nil {
		return false
	}

	return p.ProviderData.NeedMount(vol)
}
