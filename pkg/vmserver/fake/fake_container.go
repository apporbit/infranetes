package fake

import (
	"fmt"
	"sync"

	"github.com/golang/glog"

	"github.com/apporbit/infranetes/pkg/vmserver/common"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1"
)

type fakeContainerProvider struct {
	contMap map[string]*common.Container
	mapLock sync.Mutex
}

func (p *fakeContainerProvider) Lock() {
	/*	if glog.V(10) {
			glog.Infof("fakeProvider.Lock(): pre state = %v", p.mapLock)
		}
		_, file, no, ok := runtime.Caller(1)
		if ok {
			if glog.V(10) {
				glog.Infof("fakeProvider.Lock() called from %s#%d\n", file, no)
			}
		} */

	p.mapLock.Lock()
	/*	if glog.V(10) {
		glog.Infof("fakeProvider.Lock(): post state = %v", p.mapLock)
	} */
}

func (p *fakeContainerProvider) Unlock() {
	/*	if glog.V(10) {
			glog.Infof("fakeProvider.Unlock(): pre state = %v", p.mapLock)
		}
		_, file, no, ok := runtime.Caller(1)
		if ok {
			if glog.V(10) {
				glog.Infof("fakeProvider.Unlock(): called from %s#%d\n", file, no)
			}
		} */
	p.mapLock.Unlock()
	/*	if glog.V(10) {
		glog.Infof("fakeProvider.Unlock(): post state = %v", p.mapLock)
	} */
}

func (f *fakeContainerProvider) CreateContainer(req *kubeapi.CreateContainerRequest) (*kubeapi.CreateContainerResponse, error) {
	f.Lock()
	defer f.Unlock()

	glog.Infof("CreateContainer: req.Config.Image.Image = %v", req.Config.Image.Image)

	id := req.GetPodSandboxId() + ":" + req.Config.Metadata.GetName()
	f.contMap[id] = common.NewContainer(&id,
		&req.PodSandboxId,
		kubeapi.ContainerState_CONTAINER_CREATED,
		req.Config.Metadata,
		req.Config.Image,
		req.Config.Mounts,
		req.Config.Labels,
		req.Config.Annotations)

	return &kubeapi.CreateContainerResponse{ContainerId: id}, nil
}

func (f *fakeContainerProvider) StartContainer(req *kubeapi.StartContainerRequest) (*kubeapi.StartContainerResponse, error) {
	f.Lock()
	defer f.Unlock()

	id := req.GetContainerId()
	if cont, ok := f.contMap[id]; !ok {
		return nil, fmt.Errorf("StartContainer: Invalid ContainerID: %v", id)
	} else {
		cont.Start()
		return &kubeapi.StartContainerResponse{}, nil
	}
}

func (f *fakeContainerProvider) StopContainer(req *kubeapi.StopContainerRequest) (*kubeapi.StopContainerResponse, error) {
	f.Lock()
	defer f.Unlock()

	id := req.GetContainerId()
	if cont, ok := f.contMap[id]; !ok {
		return nil, fmt.Errorf("StopContainer: Invalid ContainerID: %v", id)
	} else {
		cont.Finished()
		return &kubeapi.StopContainerResponse{}, nil
	}
}

func (f *fakeContainerProvider) RemoveContainer(req *kubeapi.RemoveContainerRequest) (*kubeapi.RemoveContainerResponse, error) {
	f.Lock()
	defer f.Unlock()

	id := req.GetContainerId()
	if _, ok := f.contMap[id]; !ok {
		return nil, fmt.Errorf("RemoveContainer: Invalid ContainerID: %v", id)
	} else {
		delete(f.contMap, id)
		return &kubeapi.RemoveContainerResponse{}, nil
	}
}

func (f *fakeContainerProvider) ListContainers(req *kubeapi.ListContainersRequest) (*kubeapi.ListContainersResponse, error) {
	f.Lock()
	defer f.Unlock()

	containers := []*kubeapi.Container{}

	for _, cont := range f.contMap {
		if filter(req.Filter, cont) {
			continue
		}
		containers = append(containers, cont.ToKubeContainer())
	}

	resp := &kubeapi.ListContainersResponse{
		Containers: containers,
	}

	return resp, nil
}

func filter(filter *kubeapi.ContainerFilter, cont *common.Container) bool {
	if filter != nil {
		if filter.Id != "" && filter.GetId() == *cont.GetId() {
			glog.Infof("Filtering out %v as want %v", *cont.GetId(), filter.GetId())
			return true
		}

		if filter.State != nil && filter.GetState().State != cont.GetState() {
			glog.Infof("Filtering out %v as want %v and got %v", *cont.GetId(), filter.GetState(), cont.GetState())
			return true
		}

		if filter.PodSandboxId != "" && filter.GetPodSandboxId() != *cont.GetPodId() {
			glog.Infof("Filtering out %v as want %v and got %v", *cont.GetId(), filter.GetPodSandboxId(), *cont.GetPodId())
			return true
		}

		for k, v := range filter.GetLabelSelector() {
			if podVal, ok := cont.GetLabels()[k]; !ok {
				glog.Infof("didn't find key %v in local labels: %+v", k, cont.GetLabels())
			} else {
				if podVal != v {
					glog.Infof("Filtering out %v as want labels[%v] = %v and got %v", *cont.GetId(), k, v, podVal)
					return true
				}
			}
		}
	}

	return false
}

func (f *fakeContainerProvider) ContainerStatus(req *kubeapi.ContainerStatusRequest) (*kubeapi.ContainerStatusResponse, error) {
	f.Lock()
	defer f.Unlock()

	id := req.GetContainerId()
	if cont, ok := f.contMap[id]; !ok {
		return nil, fmt.Errorf("ContainerStatus: Invalid ContainerID: %v", id)
	} else {
		resp := &kubeapi.ContainerStatusResponse{
			Status: cont.ToKubeStatus(),
		}

		return resp, nil
	}
}
