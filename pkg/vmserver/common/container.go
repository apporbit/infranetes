package common

import (
	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
	"time"
)

type Container struct {
	id          *string
	podId       *string
	state       kubeapi.ContainerState
	metadata    *kubeapi.ContainerMetadata
	image       *kubeapi.ImageSpec
	mounts      []*kubeapi.Mount
	createdAt   int64
	startedAt   int64
	finishedAt  int64
	labels      map[string]string
	annotations map[string]string
}

func NewContainer(id *string,
	podId *string,
	state kubeapi.ContainerState,
	metadata *kubeapi.ContainerMetadata,
	image *kubeapi.ImageSpec,
	mounts []*kubeapi.Mount,
	labels map[string]string,
	annotations map[string]string) *Container {

	return &Container{
		id:          id,
		podId:       podId,
		state:       state,
		metadata:    metadata,
		image:       image,
		mounts:      mounts,
		createdAt:   time.Now().Unix(),
		labels:      labels,
		annotations: annotations,
	}
}

func (c *Container) Start() {
	c.startedAt = time.Now().Unix()
	c.state = kubeapi.ContainerState_RUNNING
}

func (c *Container) Finished() {
	c.finishedAt = time.Now().Unix()
	c.state = kubeapi.ContainerState_EXITED
}

func (c *Container) GetId() *string {
	return c.id
}

func (c *Container) GetPodId() *string {
	return c.podId
}

func (c *Container) GetLabels() map[string]string {
	return c.labels
}

func (c *Container) GetState() kubeapi.ContainerState {
	return c.state
}

func (c *Container) ToKubeContainer() *kubeapi.Container {
	ret := &kubeapi.Container{
		Annotations:  c.annotations,
		CreatedAt:    &c.createdAt,
		Id:           c.id,
		Image:        c.image,
		ImageRef:     c.image.Image,
		Labels:       c.labels,
		Metadata:     c.metadata,
		PodSandboxId: c.podId,
		State:        &c.state,
	}

	return ret
}

func (c *Container) ToKubeStatus() *kubeapi.ContainerStatus {
	exitCode := int32(0)
	var reason *string
	mounts := c.mounts

	if c.state == kubeapi.ContainerState_EXITED {
		exitCode = int32(2)
		tmp := "Error: "
		reason = &tmp
		mounts = nil
	}

	ret := &kubeapi.ContainerStatus{
		Annotations: c.annotations,
		CreatedAt:   &c.createdAt,
		ExitCode:    &exitCode,
		FinishedAt:  &c.finishedAt,
		Id:          c.id,
		Image:       c.image,
		ImageRef:    c.image.Image,
		Labels:      c.labels,
		Metadata:    c.metadata,
		Mounts:      mounts,
		Reason:      reason,
		StartedAt:   &c.startedAt,
		State:       &c.state,
	}

	return ret
}
