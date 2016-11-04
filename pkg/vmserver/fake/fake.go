package fake

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/golang/glog"

	"github.com/sjpotter/infranetes/pkg/vmserver"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

type fakeProvider struct {
	contMap map[string]*container
	mapLock sync.Mutex
}

func init() {
	vmserver.ContainerProviders.RegisterProvider("fake", NewFakeProvider)
}

func NewFakeProvider() (vmserver.ContainerProvider, error) {
	fakeProvider := &fakeProvider{
		contMap: make(map[string]*container),
	}

	return fakeProvider, nil
}

func (f *fakeProvider) CreateContainer(req *kubeapi.CreateContainerRequest) (*kubeapi.CreateContainerResponse, error) {
	f.mapLock.Lock()
	defer f.mapLock.Unlock()

	id := req.GetPodSandboxId() + ":" + req.Config.Metadata.GetName()
	f.contMap[id] = &container{
		id:          &id,
		podId:       req.PodSandboxId,
		state:       kubeapi.ContainerState_CREATED,
		metadata:    req.Config.Metadata,
		image:       req.Config.Image,
		mounts:      req.Config.Mounts,
		createdAt:   time.Now().Unix(),
		labels:      req.Config.Labels,
		annotations: req.Config.Annotations,
	}

	return &kubeapi.CreateContainerResponse{ContainerId: &id}, nil
}

func (f *fakeProvider) StartContainer(req *kubeapi.StartContainerRequest) (*kubeapi.StartContainerResponse, error) {
	f.mapLock.Lock()
	defer f.mapLock.Unlock()

	id := req.GetContainerId()
	if cont, ok := f.contMap[id]; !ok {
		return nil, fmt.Errorf("StartContainer: Invalid ContainerID: %v", id)
	} else {
		cont.state = kubeapi.ContainerState_RUNNING
		cont.startedAt = time.Now().Unix()
		return &kubeapi.StartContainerResponse{}, nil
	}
}

func (f *fakeProvider) StopContainer(req *kubeapi.StopContainerRequest) (*kubeapi.StopContainerResponse, error) {
	f.mapLock.Lock()
	defer f.mapLock.Unlock()

	id := req.GetContainerId()
	if cont, ok := f.contMap[id]; !ok {
		return nil, fmt.Errorf("StopContainer: Invalid ContainerID: %v", id)
	} else {
		cont.state = kubeapi.ContainerState_EXITED
		cont.FinishedAt = time.Now().Unix()
		return &kubeapi.StopContainerResponse{}, nil
	}
}

func (f *fakeProvider) RemoveContainer(req *kubeapi.RemoveContainerRequest) (*kubeapi.RemoveContainerResponse, error) {
	f.mapLock.Lock()
	defer f.mapLock.Unlock()

	id := req.GetContainerId()
	if _, ok := f.contMap[id]; !ok {
		return nil, fmt.Errorf("RemoveContainer: Invalid ContainerID: %v", id)
	} else {
		delete(f.contMap, id)
		return &kubeapi.RemoveContainerResponse{}, nil
	}
}

func (f *fakeProvider) ListContainers(req *kubeapi.ListContainersRequest) (*kubeapi.ListContainersResponse, error) {
	f.mapLock.Lock()
	defer f.mapLock.Unlock()

	containers := []*kubeapi.Container{}

	for _, cont := range f.contMap {
		if filter(req.Filter, cont) {
			continue
		}
		containers = append(containers, cont.toKubeContainer())
	}

	resp := &kubeapi.ListContainersResponse{
		Containers: containers,
	}

	return resp, nil
}

func filter(filter *kubeapi.ContainerFilter, cont *container) bool {
	if filter != nil {
		if filter.GetId() != "" && filter.GetId() == *cont.id {
			glog.Infof("Filtering out %v as want %v", *cont.id, filter.GetId())
			return true
		}

		if filter.GetState() == cont.state {
			glog.Infof("Filtering out %v as want %v and got %v", *cont.id, filter.GetState(), cont.state)
			return true
		}

		if filter.GetPodSandboxId() != "" && filter.GetPodSandboxId() != *cont.podId {
			glog.Infof("Filtering out %v as want %v and got %v", *cont.id, filter.GetPodSandboxId(), *cont.podId)
			return true
		}

		for k, v := range filter.GetLabelSelector() {
			if podVal, ok := cont.labels[k]; !ok {
				glog.Infof("didn't find key %v in local labels: %+v", k, cont.labels)
			} else {
				if podVal != v {
					glog.Infof("Filtering out %v as want labels[%v] = %v and got %v", *cont.id, k, v, podVal)
					return true
				}
			}
		}
	}

	return false
}

func (f *fakeProvider) ContainerStatus(req *kubeapi.ContainerStatusRequest) (*kubeapi.ContainerStatusResponse, error) {
	f.mapLock.Lock()
	defer f.mapLock.Unlock()

	id := req.GetContainerId()
	if cont, ok := f.contMap[id]; !ok {
		return nil, fmt.Errorf("ContainerStatus: Invalid ContainerID: %v", id)
	} else {
		resp := &kubeapi.ContainerStatusResponse{
			Status: cont.toKubeStatus(),
		}

		return resp, nil
	}
}

func (f *fakeProvider) Exec(_ kubeapi.RuntimeService_ExecServer) error {
	return errors.New("unimplemented")
}