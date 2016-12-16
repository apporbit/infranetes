package fake

import (
	"github.com/golang/glog"

	"github.com/sjpotter/infranetes/pkg/vmserver"
	"github.com/sjpotter/infranetes/pkg/vmserver/common"
)

type fakeProvider struct {
	*fakeContainerProvider
	*fakeExecProvider
}

func NewFakeProvider() (vmserver.ContainerProvider, error) {
	glog.Info("NewFakeProvider: starting")
	fake := &fakeProvider{
		fakeContainerProvider: &fakeContainerProvider{
			contMap: make(map[string]*common.Container),
		},
		fakeExecProvider: &fakeExecProvider{},
	}

	return fake, nil
}

type execProvider struct {
	*fakeContainerProvider
	*podExecProvider
}

func NewPodExecProvider() (vmserver.ContainerProvider, error) {
	glog.Info("NewPodExecProvider: starting")
	fake := &execProvider{
		fakeContainerProvider: &fakeContainerProvider{
			contMap: make(map[string]*common.Container),
		},
		podExecProvider: &podExecProvider{},
	}

	return fake, nil
}

func init() {
	vmserver.ContainerProviders.RegisterProvider("fake", NewFakeProvider)
	vmserver.ContainerProviders.RegisterProvider("podexec", NewPodExecProvider)
}
