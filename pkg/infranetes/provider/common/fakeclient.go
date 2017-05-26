package common

import (
	"errors"

	"github.com/sjpotter/infranetes/pkg/common"
	"github.com/sjpotter/infranetes/pkg/vmserver"
	"github.com/sjpotter/infranetes/pkg/vmserver/fake"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1"
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

func (c *fakeClient) ExecSync(req *kubeapi.ExecSyncRequest) (*kubeapi.ExecSyncResponse, error) {
	return c.fakeProvider.ExecSync(req)
}

func (c *fakeClient) Exec(req *kubeapi.ExecRequest) (*kubeapi.ExecResponse, error) {
	return nil, errors.New("fake doesn't support streaming Exec")
}

func (c *fakeClient) Attach(req *kubeapi.AttachRequest) (*kubeapi.AttachResponse, error) {
	return nil, errors.New("fake doesn't support streaming attach")
}

func (c *fakeClient) PortForward(req *kubeapi.PortForwardRequest) (*kubeapi.PortForwardResponse, error) {
	return nil, errors.New("fake doesn't support streaming attach")
}

func (c *fakeClient) Version() (*kubeapi.VersionResponse, error) {
	return &kubeapi.VersionResponse{}, nil
}

func (c *fakeClient) Ready() error {
	return nil
}

func (c *fakeClient) StartProxy() error {
	return errors.New("Fake doesn't support StartProxy")
}

func (c *fakeClient) RunCmd(req *common.RunCmdRequest) error {
	return errors.New("Fake doesn't support RunCmd")
}

func (c *fakeClient) SetPodIP(ip string) error {
	return errors.New("Fake doesn't support SetPodIP")
}

func (c *fakeClient) GetPodIP() (string, error) {
	return "", errors.New("Fake doesn't support GetPodIP")
}

func (c *fakeClient) SetSandboxConfig(config *kubeapi.PodSandboxConfig) error {
	return errors.New("Fake doesn't support SetSandboxConfig")
}

func (c *fakeClient) GetSandboxConfig() (*kubeapi.PodSandboxConfig, error) {
	return nil, errors.New("Fake doesn't support GetSandboxConfig")
}

func (c *fakeClient) CopyFile(file string) error {
	return nil
}

func (c *fakeClient) MountFs(source string, target string, fstype string, readOnly bool) error {
	return nil
}

func (c *fakeClient) UnmountFs(target string) error {
	return nil
}

func (c *fakeClient) SetHostname(hostname string) error {
	return errors.New("Fake doesn't support RunCmd")
}

func (c *fakeClient) Close() {
}

func (c *fakeClient) SaveLogs(container string, path string) error {
	return nil
}

func (c *fakeClient) GetMetric(req *common.GetMetricsRequest) (*common.GetMetricsResponse, error) {
	return &common.GetMetricsResponse{}, nil
}

func (c *fakeClient) AddRoute(req *common.AddRouteRequest) (*common.AddRouteResponse, error) {
	return &common.AddRouteResponse{}, nil
}
