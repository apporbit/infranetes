package fake

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/sjpotter/infranetes/pkg/vmserver"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

type container struct {
	id          *string
	podId       *string
	state       kubeapi.ContainerState
	metadata    *kubeapi.ContainerMetadata
	image       *kubeapi.ImageSpec
	mounts      []*kubeapi.Mount
	createdAt   int64
	startedAt   int64
	FinishedAt  int64
	labels      map[string]string
	annotations map[string]string
}

func (c *container) toKubeContainer() *kubeapi.Container {
	return &kubeapi.Container{
		PodSandboxId: c.podId,
		Metadata:     c.metadata,
		Annotations:  c.annotations,
		CreatedAt:    &c.createdAt,
		Id:           c.id,
		Image:        c.image,
		ImageRef:     c.image.Image,
		Labels:       c.labels,
		State:        &c.state,
	}
}

func (c *container) toKubeStatus() *kubeapi.ContainerStatus {
	exitCode := int32(0)
	var reason string
	if c.state == kubeapi.ContainerState_EXITED {
		reason = "Completed"
	}
	return &kubeapi.ContainerStatus{
		Id:          c.id,
		Metadata:    c.metadata,
		Image:       c.image,
		ImageRef:    c.image.Image,
		Mounts:      c.mounts,
		ExitCode:    &exitCode,
		State:       &c.state,
		CreatedAt:   &c.createdAt,
		StartedAt:   &c.startedAt,
		FinishedAt:  &c.FinishedAt,
		Reason:      &reason,
		Labels:      c.labels,
		Annotations: c.annotations,
	}
}

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

func (d *fakeProvider) CreateContainer(req *kubeapi.CreateContainerRequest) (*kubeapi.CreateContainerResponse, error) {
	d.mapLock.Lock()
	defer d.mapLock.Unlock()

	id := req.GetPodSandboxId() + ":" + req.Config.Metadata.GetName()
	d.contMap[id] = &container{
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

func (d *fakeProvider) StartContainer(req *kubeapi.StartContainerRequest) (*kubeapi.StartContainerResponse, error) {
	d.mapLock.Lock()
	defer d.mapLock.Unlock()

	id := req.GetContainerId()
	if cont, ok := d.contMap[id]; !ok {
		return nil, fmt.Errorf("StartContainer: Invalid ContainerID: %v", id)
	} else {
		cont.state = kubeapi.ContainerState_RUNNING
		cont.startedAt = time.Now().Unix()
		return &kubeapi.StartContainerResponse{}, nil
	}
}

func (d *fakeProvider) StopContainer(req *kubeapi.StopContainerRequest) (*kubeapi.StopContainerResponse, error) {
	d.mapLock.Lock()
	defer d.mapLock.Unlock()

	id := req.GetContainerId()
	if cont, ok := d.contMap[id]; !ok {
		return nil, fmt.Errorf("StopContainer: Invalid ContainerID: %v", id)
	} else {
		cont.state = kubeapi.ContainerState_EXITED
		cont.FinishedAt = time.Now().Unix()
		return &kubeapi.StopContainerResponse{}, nil
	}

}

func (d *fakeProvider) RemoveContainer(req *kubeapi.RemoveContainerRequest) (*kubeapi.RemoveContainerResponse, error) {
	d.mapLock.Lock()
	defer d.mapLock.Unlock()

	id := req.GetContainerId()
	if _, ok := d.contMap[id]; !ok {
		return nil, fmt.Errorf("RemoveContainer: Invalid ContainerID: %v", id)
	} else {
		delete(d.contMap, id)
		return &kubeapi.RemoveContainerResponse{}, nil
	}
}

func (d *fakeProvider) ListContainers(req *kubeapi.ListContainersRequest) (*kubeapi.ListContainersResponse, error) {
	d.mapLock.Lock()
	defer d.mapLock.Unlock()

	containers := []*kubeapi.Container{}

	for _, cont := range d.contMap {
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
			return true
		}

		if filter.GetState() == cont.state {
			return true
		}

		for k, v := range filter.GetLabelSelector() {
			if cont.labels[k] != v {
				return true
			}
		}
	}

	return false
}

func (d *fakeProvider) ContainerStatus(req *kubeapi.ContainerStatusRequest) (*kubeapi.ContainerStatusResponse, error) {
	d.mapLock.Lock()
	defer d.mapLock.Unlock()

	id := req.GetContainerId()
	if cont, ok := d.contMap[id]; !ok {
		return nil, fmt.Errorf("ContainerStatus: Invalid ContainerID: %v", id)
	} else {
		resp := &kubeapi.ContainerStatusResponse{
			Status: cont.toKubeStatus(),
		}

		return resp, nil
	}
}

func (d *fakeProvider) Exec(_ kubeapi.RuntimeService_ExecServer) error {
	return errors.New("unimplemented")
}
