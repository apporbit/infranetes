package infranetes

import (
	"errors"
	"fmt"

	"github.com/golang/glog"

	"github.com/sjpotter/infranetes/pkg/infranetes/provider/common"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

func (m *Manager) importSandboxes() {
	podDatas, err := m.podProvider.ListInstances()

	if err != nil {
		return
	}

	m.vmMapLock.Lock()
	defer m.vmMapLock.Unlock()

	for _, podData := range podDatas {
		m.vmMap[*podData.Id] = podData
	}
}

func (m *Manager) createSandbox(req *kubeapi.RunPodSandboxRequest) (*kubeapi.RunPodSandboxResponse, error) {
	resp := &kubeapi.RunPodSandboxResponse{}

	podData, err := m.podProvider.RunPodSandbox(req)
	if err == nil {
		m.vmMapLock.Lock()
		defer m.vmMapLock.Unlock()

		m.vmMap[*podData.Id] = podData

		resp.PodSandboxId = podData.Id
	}

	return resp, err
}

func (m *Manager) stopSandbox(req *kubeapi.StopPodSandboxRequest) (*kubeapi.StopPodSandboxResponse, error) {
	podId := req.GetPodSandboxId()

	podData, err := m.getPodData(podId)
	if err != nil {
		msg := fmt.Sprintf("stopSandbox: couldn't get podData for %s: %v", podId, err)
		glog.Infof(msg)
		return nil, fmt.Errorf(msg)
	}

	podData.Lock()
	defer podData.Unlock()

	// Should turn this into a single call to the VM - i.e. StopAllContainers()

	client := podData.Client
	if client == nil { // This sandbox has been stopped
		msg := fmt.Sprintf("stopSandbox: got nil client for %s: %v", podId)
		glog.Warning(msg)
		return nil, fmt.Errorf(msg)
	}

	contResp, err := client.ListContainers(&kubeapi.ListContainersRequest{})
	if err != nil {
		msg := fmt.Sprintf("stopSandbox: ListContainers failed for %s: %v", podId, err)
		glog.Infof(msg)
		return nil, errors.New(msg)
	}

	for _, cont := range contResp.Containers {
		timeout := int64(60)
		contReq := &kubeapi.StopContainerRequest{
			ContainerId: cont.Id,
			Timeout:     &timeout,
		}
		if _, err := client.StopContainer(contReq); err != nil {
			glog.Warningf("stopSandbox: StopContainer failed in pod %s for container %s: %v", podId, *cont.Id, err)
			continue
		}
	}

	podData.StopPod()
	m.podProvider.StopPodSandbox(podData)

	resp := &kubeapi.StopPodSandboxResponse{}

	return resp, nil
}

func (m *Manager) removePodSandbox(req *kubeapi.RemovePodSandboxRequest) error {
	podData, err := m.getPodData(req.GetPodSandboxId())
	if err != nil {
		return fmt.Errorf("removePodSandbox: %v", err)
	}

	podData.Lock()
	defer podData.Unlock()

	if err := podData.VM.Destroy(); err != nil {
		return fmt.Errorf("removePodSandbox: %v", err)
	}

	podData.RemovePod()
	m.podProvider.RemovePodSandbox(podData)

	m.vmMapLock.Lock()
	defer m.vmMapLock.Unlock()

	delete(m.vmMap, req.GetPodSandboxId())

	return nil
}

func (m *Manager) podSandboxStatus(req *kubeapi.PodSandboxStatusRequest) (*kubeapi.PodSandboxStatusResponse, error) {
	podData, err := m.getPodData(req.GetPodSandboxId())
	if err != nil {
		return nil, fmt.Errorf("PodSandboxStatus: %v", err)
	}
	podData.RLock()
	defer podData.RUnlock()

	status := podData.PodStatus()

	resp := &kubeapi.PodSandboxStatusResponse{
		Status: status,
	}

	return resp, nil
}

func (m *Manager) listPodSandbox(req *kubeapi.ListPodSandboxRequest) (*kubeapi.ListPodSandboxResponse, error) {
	sandboxes := []*kubeapi.PodSandbox{}

	glog.V(1).Infof("listPodSandbox: len of vmMap = %v", len(m.vmMap))

	for _, podData := range m.copyVMMap() {
		// podData lock is taken and released in filter
		if sandbox, ok := m.filter(podData, req.Filter); ok {
			glog.V(1).Infof("listPodSandbox Appending a sandbox for %v to sandboxes", *podData.Id)
			sandboxes = append(sandboxes, sandbox)
		}
	}

	glog.V(1).Infof("ListPodSandbox: len of sandboxes returning = %v", len(sandboxes))

	resp := &kubeapi.ListPodSandboxResponse{
		Items: sandboxes,
	}

	return resp, nil
}

func (m *Manager) filter(podData *common.PodData, reqFilter *kubeapi.PodSandboxFilter) (*kubeapi.PodSandbox, bool) {
	podData.RLock()
	defer podData.RUnlock()

	glog.V(1).Infof("filter: podData for %v = %+v", *podData.Id, podData)

	if filter, msg := podData.Filter(reqFilter); filter {
		glog.V(1).Infof("filter: filtering out %v on labels as %v", *podData.Id, msg)
		return nil, false
	}

	sandbox := podData.GetSandbox()

	return sandbox, true
}

func (m *Manager) preCreateContainer(data *common.PodData, req *kubeapi.CreateContainerRequest) error {
	data.RLock()
	defer data.RUnlock()

	return m.podProvider.PreCreateContainer(data, req, m.contProvider.ImageStatus)
}

func (m *Manager) listContainers(req *kubeapi.ListContainersRequest) (*kubeapi.ListContainersResponse, error) {
	results := []*kubeapi.Container{}

	for _, podData := range m.copyVMMap() {
		if containers, ok := listSandbox(req, podData); ok {
			results = append(results, containers...)
		}
	}

	resp := &kubeapi.ListContainersResponse{
		Containers: results,
	}

	return resp, nil
}

func listSandbox(req *kubeapi.ListContainersRequest, podData *common.PodData) ([]*kubeapi.Container, bool) {
	podData.RLock()
	defer podData.RUnlock()

	sandboxId := ""
	if req.Filter != nil {
		sandboxId = req.Filter.GetPodSandboxId()
	}
	if sandboxId != "" && sandboxId != *podData.Id {
		return nil, false
	}

	client := podData.Client
	if client == nil { // This sandbox has been removed
		return nil, false
	}

	resp, err := client.ListContainers(req)
	if err != nil {
		glog.Warningf("listContainers: grpc ListContainers failed: %v", err)
		return nil, false
	}

	return resp.Containers, true
}

/* Must be at least holding the vmmap RLock */
func (m *Manager) getPodData(id string) (*common.PodData, error) {
	m.vmMapLock.RLock()
	defer m.vmMapLock.RUnlock()

	podData, ok := m.vmMap[id]
	if !ok {
		return nil, fmt.Errorf("Invalid PodSandboxId (%v)", id)
	}
	return podData, nil
}

func (m *Manager) getClient(podName string) (common.Client, error) {
	m.vmMapLock.RLock()
	defer m.vmMapLock.RUnlock()

	return m.getClientLocked(podName)
}

func (m *Manager) getClientLocked(podName string) (common.Client, error) {
	podData, err := m.getPodData(podName)

	if err != nil {
		return nil, fmt.Errorf("%v unknown pod name", podName)
	}

	return podData.Client, nil
}

func (m *Manager) getVMList() []string {
	m.vmMapLock.RLock()
	defer m.vmMapLock.RUnlock()

	ret := []string{}

	for name := range m.vmMap {
		ret = append(ret, name)
	}

	return ret
}

func (m *Manager) copyVMMap() map[string]*common.PodData {
	m.vmMapLock.RLock()
	defer m.vmMapLock.RUnlock()

	ret := make(map[string]*common.PodData, len(m.vmMap))
	for key, val := range m.vmMap {
		ret[key] = val
	}

	return ret
}

func (m *Manager) updatePodState(data *common.PodData) {
	if data.Booted {
		data.UpdatePodState()
	}
}
