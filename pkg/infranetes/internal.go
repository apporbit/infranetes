package infranetes

import (
	"fmt"

	"github.com/sjpotter/infranetes/pkg/infranetes/provider/common"

	"github.com/golang/glog"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

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

	m.vmMapLock.RLock()
	defer m.vmMapLock.RUnlock()

	podData, err := m.getPodData(podId)
	if err != nil {
		msg := fmt.Sprintf("stopSandbox: couldn't get podData for %s: %v", podId, err)
		glog.Infof(msg)
		return nil, fmt.Errorf(msg)
	}

	podData.StateLock.Lock()
	defer podData.StateLock.Unlock()

	client := podData.Client
	if client == nil { // This sandbox has been stopped
		msg := fmt.Sprintf("stopSandbox: got nil client for %s: %v", podId, err)
		glog.Infof(msg)
		return nil, fmt.Errorf(msg)
	}

	contResp, err := client.ListContainers(&kubeapi.ListContainersRequest{})
	if err != nil {
		msg := fmt.Sprintf("stopSandbox: ListContainers failed for %s: %v", podId, err)
		glog.Infof(msg)
		return nil, fmt.Errorf(msg)
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
	m.vmMapLock.Lock()
	defer m.vmMapLock.Unlock()

	podData, err := m.getPodData(req.GetPodSandboxId())
	if err != nil {
		return fmt.Errorf("removePodSandbox: %v", err)
	}

	podData.StateLock.Lock()
	defer podData.StateLock.Unlock()

	if err := podData.VM.Destroy(); err != nil {
		return fmt.Errorf("removePodSandbox: %v", err)
	}

	podData.RemovePod()
	m.podProvider.RemovePodSandbox(podData)

	delete(m.vmMap, req.GetPodSandboxId())

	return nil
}

func (m *Manager) podSandboxStatus(req *kubeapi.PodSandboxStatusRequest) (*kubeapi.PodSandboxStatusResponse, error) {
	m.vmMapLock.RLock()
	defer m.vmMapLock.RUnlock()

	podData, err := m.getPodData(req.GetPodSandboxId())
	if err != nil {
		return nil, fmt.Errorf("PodSandboxStatus: %v", err)
	}

	podData.StateLock.Lock()
	defer podData.StateLock.Unlock()

	m.podProvider.UpdatePodState(podData)

	status := podData.PodStatus()

	resp := &kubeapi.PodSandboxStatusResponse{
		Status: status,
	}

	return resp, nil
}

func (m *Manager) listPodSandbox(req *kubeapi.ListPodSandboxRequest) (*kubeapi.ListPodSandboxResponse, error) {
	m.vmMapLock.RLock()
	defer m.vmMapLock.RUnlock()

	sandboxes := []*kubeapi.PodSandbox{}

	glog.V(1).Infof("listPodSandbox: len of vmMap = %v", len(m.vmMap))

	for id, podData := range m.vmMap {
		// podData lock is taken and released in filter
		if sandbox, ok := m.filter(podData, req.Filter); ok {
			glog.V(1).Infof("listPodSandbox Appending a sandbox for %v to sandboxes", id)
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
	podData.StateLock.Lock()
	defer podData.StateLock.Unlock()

	glog.V(1).Infof("filter: podData for %v = %+v", *podData.Id, podData)

	m.podProvider.UpdatePodState(podData)

	if filter, msg := podData.Filter(reqFilter); filter {
		glog.V(1).Infof("filter: filtering out %v on labels as %v", *podData.Id, msg)
		return nil, false
	}

	sandbox := podData.GetSandbox()

	return sandbox, true
}

func (m *Manager) listContainers(req *kubeapi.ListContainersRequest) (*kubeapi.ListContainersResponse, error) {
	results := []*kubeapi.Container{}

	m.vmMapLock.RLock()
	defer m.vmMapLock.RUnlock()

	for _, podId := range m.getVMList() {
		sandboxId := ""
		if req.Filter != nil {
			sandboxId = req.Filter.GetPodSandboxId()
		}
		if sandboxId == "" || sandboxId == podId {
			client, err := m.getClientLocked(podId)
			if err != nil {
				glog.Warningf("ListContainers: couldn't get client for %s: %v", podId, err)
				continue
			}
			if client == nil { // This sandbox has been stopped
				continue
			}

			resp, err := client.ListContainers(req)
			if err != nil {
				glog.Warningf("listContainers: grpc ListContainers failed: %v", err)
				continue
			}

			results = append(results, resp.Containers...)
		}
	}

	resp := &kubeapi.ListContainersResponse{
		Containers: results,
	}

	return resp, nil
}

/* Must be at least holding the vmmap RLock */
func (m *Manager) getPodData(id string) (*common.PodData, error) {
	podData, ok := m.vmMap[id]
	if !ok {
		return nil, fmt.Errorf("Invalid PodSandboxId (%v)", id)
	}
	return podData, nil
}

func (m *Manager) getClient(podName string) (*common.Client, error) {
	m.vmMapLock.RLock()
	defer m.vmMapLock.RUnlock()

	return m.getClientLocked(podName)
}

func (m *Manager) getClientLocked(podName string) (*common.Client, error) {
	podData, err := m.getPodData(podName)

	if err != nil {
		return nil, fmt.Errorf("%v unknown pod name", podName)
	}

	return podData.Client, nil
}

func (v *Manager) getVMList() []string {
	ret := []string{}

	for name := range v.vmMap {
		ret = append(ret, name)
	}

	return ret
}
