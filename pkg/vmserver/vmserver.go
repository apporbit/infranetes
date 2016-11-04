package vmserver

import (
	"errors"
	"fmt"
	"net"

	"github.com/golang/glog"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/sjpotter/infranetes/pkg/common"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

type VMserver struct {
	contProvider ContainerProvider
	server       *grpc.Server
	podIp        *string
	config       []byte
}

func NewVMServer(cert *string, key *string, contProvider ContainerProvider) (*VMserver, error) {
	var opts []grpc.ServerOption
	creds, err := credentials.NewServerTLSFromFile(*cert, *key)
	if err != nil {
		return nil, err
	}
	opts = []grpc.ServerOption{grpc.Creds(creds)}
	manager := &VMserver{
		contProvider: contProvider,
		server:       grpc.NewServer(opts...),
	}

	manager.registerServer()

	return manager, nil
}

func (s *VMserver) registerServer() {
	kubeapi.RegisterRuntimeServiceServer(s.server, s)
	common.RegisterVMServerServer(s.server, s)
}

func (s *VMserver) Serve(port int) error {
	glog.V(1).Infof("Start infranetes on port %d", port)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))

	if err != nil {
		glog.Fatalf("Failed to listen on port %d: %v", port, err)
		return err
	}

	return s.server.Serve(lis)
}

var (
	runtimeAPIVersion = "0.1.0"
)

func (s *VMserver) Version(ctx context.Context, req *kubeapi.VersionRequest) (*kubeapi.VersionResponse, error) {
	runtimeName := "infranetes"

	resp := &kubeapi.VersionResponse{
		RuntimeApiVersion: &runtimeAPIVersion,
		RuntimeName:       &runtimeName,
		RuntimeVersion:    &runtimeAPIVersion,
		Version:           &runtimeAPIVersion,
	}

	return resp, nil
}

func (m *VMserver) RunPodSandbox(ctx context.Context, req *kubeapi.RunPodSandboxRequest) (*kubeapi.RunPodSandboxResponse, error) {
	return nil, errors.New("Not Implemented")
}

func (m *VMserver) StopPodSandbox(ctx context.Context, req *kubeapi.StopPodSandboxRequest) (*kubeapi.StopPodSandboxResponse, error) {
	return nil, errors.New("Not Implemented")
}

func (m *VMserver) RemovePodSandbox(ctx context.Context, req *kubeapi.RemovePodSandboxRequest) (*kubeapi.RemovePodSandboxResponse, error) {
	return nil, errors.New("Not Implemented")
}

func (m *VMserver) PodSandboxStatus(ctx context.Context, req *kubeapi.PodSandboxStatusRequest) (*kubeapi.PodSandboxStatusResponse, error) {
	return nil, errors.New("Not Implemented")
}

func (m *VMserver) ListPodSandbox(ctx context.Context, req *kubeapi.ListPodSandboxRequest) (*kubeapi.ListPodSandboxResponse, error) {
	return nil, errors.New("Not Implemented")
}

func (m *VMserver) CreateContainer(ctx context.Context, req *kubeapi.CreateContainerRequest) (*kubeapi.CreateContainerResponse, error) {
	glog.Infof("CreateContainer: req = %+v", req)

	resp, err := m.contProvider.CreateContainer(req)

	glog.Infof("CreateContainer: resp = %+v, err = %v", resp, err)

	return resp, err
}

func (m *VMserver) StartContainer(ctx context.Context, req *kubeapi.StartContainerRequest) (*kubeapi.StartContainerResponse, error) {
	glog.Infof("StartContainer: req = %+v", req)

	resp, err := m.contProvider.StartContainer(req)

	glog.Infof("StartContainer: resp = %+v, err = %v", resp, err)

	return resp, err
}

func (m *VMserver) StopContainer(ctx context.Context, req *kubeapi.StopContainerRequest) (*kubeapi.StopContainerResponse, error) {
	glog.Infof("StopContainer: req = %+v", req)

	resp, err := m.contProvider.StopContainer(req)

	glog.Infof("StopContainer: resp = %+v, err = %v", resp, err)

	return resp, err
}

func (m *VMserver) RemoveContainer(ctx context.Context, req *kubeapi.RemoveContainerRequest) (*kubeapi.RemoveContainerResponse, error) {
	glog.Infof("RemoveContainer: req = %+v", req)

	resp, err := m.contProvider.RemoveContainer(req)

	glog.Infof("RemoveContainer: resp = %+v, err = %v", resp, err)

	return resp, err
}

func (m *VMserver) ListContainers(ctx context.Context, req *kubeapi.ListContainersRequest) (*kubeapi.ListContainersResponse, error) {
	glog.Infof("ListContainers: req = %+v", req)

	resp, err := m.contProvider.ListContainers(req)

	glog.Infof("ListContainers: resp = %+v, err = %v", resp, nil)

	return resp, err
}

func (m *VMserver) ContainerStatus(ctx context.Context, req *kubeapi.ContainerStatusRequest) (*kubeapi.ContainerStatusResponse, error) {
	glog.Infof("ContainerStatus: req = %+v", req)

	resp, err := m.contProvider.ContainerStatus(req)

	glog.Infof("ContainerStatus: resp = %+v, err = %v", resp, err)

	return resp, err
}

func (m *VMserver) Exec(stream kubeapi.RuntimeService_ExecServer) error {
	glog.Infof("Exec: Enter")

	err := m.contProvider.Exec(stream)

	glog.Infof("Exec: err = %v", err)

	return err
}
