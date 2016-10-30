package fake

import (
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

func (c *container) toKubeStatus() *kubeapi.ContainerStatus {
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
		FinishedAt:  &c.FinishedAt,
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
