package common

import (
	"fmt"

	"github.com/sjpotter/infranetes/pkg/common"
	"github.com/sjpotter/infranetes/pkg/vmserver"
	"github.com/sjpotter/infranetes/pkg/vmserver/fake"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

type fakeClient struct {
	fakeProvider vmserver.ContainerProvider
}

func CreateFakeClient() (Client, error) {
	provider, _ := fake.NewFakeProvider()
	client := &fakeClient{
		fakeProvider: provider,
	}

	return client, nil
}

func (c *fakeClient) CreateContainer(req *kubeapi.CreateContainerRequest) (*kubeapi.CreateContainerResponse, error) {
	return c.fakeProvider.CreateContainer(req)
}

func (c *fakeClient) StartContainer(req *kubeapi.StartContainerRequest) (*kubeapi.StartContainerResponse, error) {
	return c.fakeProvider.StartContainer(req)
}

func (c *fakeClient) StopContainer(req *kubeapi.StopContainerRequest) (*kubeapi.StopContainerResponse, error) {
	return c.fakeProvider.StopContainer(req)
}

func (c *fakeClient) RemoveContainer(req *kubeapi.RemoveContainerRequest) (*kubeapi.RemoveContainerResponse, error) {
	return c.fakeProvider.RemoveContainer(req)
}

func (c *fakeClient) ListContainers(req *kubeapi.ListContainersRequest) (*kubeapi.ListContainersResponse, error) {
	return c.fakeProvider.ListContainers(req)
}

func (c *fakeClient) ContainerStatus(req *kubeapi.ContainerStatusRequest) (*kubeapi.ContainerStatusResponse, error) {
	return c.fakeProvider.ContainerStatus(req)
}

func (c *fakeClient) StartProxy() error {
	return fmt.Errorf("Fake doesn't support StartProxy")
}

func (c *fakeClient) RunCmd(req *common.RunCmdRequest) error {
	return fmt.Errorf("Fake doesn't support RunCmd")
}

func (c *fakeClient) SetPodIP(ip string) error {
	return fmt.Errorf("Fake doesn't support RunCmd")
}

func (c *fakeClient) GetPodIP() (string, error) {
	return "", fmt.Errorf("Fake doesn't support RunCmd")
}

func (c *fakeClient) SetSandboxConfig(config *kubeapi.PodSandboxConfig) error {
	return fmt.Errorf("Fake doesn't support RunCmd")
}

func (c *fakeClient) GetSandboxConfig() (*kubeapi.PodSandboxConfig, error) {
	return nil, fmt.Errorf("Fake doesn't support RunCmd")
}

func (c *fakeClient) CopyFile(file string) error {
	return nil
}

func (c *fakeClient) MountFs(source string, target string, fstype string, readOnly bool) error {
	return nil
}

func (c *fakeClient) SetHostname(hostname string) error {
	return fmt.Errorf("Fake doesn't support RunCmd")
}
func (c *fakeClient) Close() {
}
