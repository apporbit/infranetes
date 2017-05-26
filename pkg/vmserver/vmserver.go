package vmserver

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/golang/glog"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/google/cadvisor/cache/memory"
	cadvisormetrics "github.com/google/cadvisor/container"
	cadvisorapiv2 "github.com/google/cadvisor/info/v2"
	"github.com/google/cadvisor/manager"
	"github.com/google/cadvisor/utils/sysfs"
	"k8s.io/kubernetes/pkg/kubelet/server/streaming"
	"k8s.io/kubernetes/pkg/kubelet/types"

	"github.com/sjpotter/infranetes/pkg/common"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1"
)

const statsCacheDuration = 2 * time.Minute
const maxHousekeepingInterval = 15 * time.Second
const defaultHousekeepingInterval = 10 * time.Second
const allowDynamicHousekeeping = true

func init() {
	// Override cAdvisor flag defaults.
	flagOverrides := map[string]string{
		// Override the default cAdvisor housekeeping interval.
		"housekeeping_interval": defaultHousekeepingInterval.String(),
		// Disable event storage by default.
		"event_storage_event_limit": "default=0",
		"event_storage_age_limit":   "default=0",
	}
	for name, defaultValue := range flagOverrides {
		if f := flag.Lookup(name); f != nil {
			f.DefValue = defaultValue
			f.Value.Set(defaultValue)
		} else {
			glog.Errorf("Expected cAdvisor flag %q not found", name)
		}
	}
}

type VMserver struct {
	contProvider    ContainerProvider
	server          *grpc.Server
	podIp           *string
	config          *kubeapi.PodSandboxConfig
	streamingServer streaming.Server
	cadvisor        manager.Manager
}

func NewVMServer(cert *string, key *string, contProvider ContainerProvider) (*VMserver, error) {
	var opts []grpc.ServerOption
	creds, err := credentials.NewServerTLSFromFile(*cert, *key)
	if err != nil {
		return nil, err
	}
	opts = []grpc.ServerOption{grpc.Creds(creds)}

	sysFs := sysfs.NewRealSysFs()
	if err != nil {
		return nil, fmt.Errorf("Couldn't create sysfs object: %v", err)
	}
	m, err := manager.New(memory.New(statsCacheDuration, nil), sysFs, maxHousekeepingInterval, allowDynamicHousekeeping, cadvisormetrics.MetricSet{cadvisormetrics.NetworkTcpUsageMetrics: struct{}{}}, http.DefaultClient)
	if err != nil {
		return nil, fmt.Errorf("Couldn't create cadvisor manager: %v", err)
	}
	err = m.Start()
	if err != nil {
		return nil, fmt.Errorf("Couldn't start cadvisor manager")
	}

	manager := &VMserver{
		contProvider: contProvider,
		server:       grpc.NewServer(opts...),
		cadvisor:     m,
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
		RuntimeApiVersion: runtimeAPIVersion,
		RuntimeName:       runtimeName,
		RuntimeVersion:    runtimeAPIVersion,
		Version:           runtimeAPIVersion,
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
	glog.V(10).Infof("ListContainers: req = %+v", req)

	resp, err := m.contProvider.ListContainers(req)

	glog.V(10).Infof("ListContainers: resp = %+v, err = %v", resp, nil)

	return resp, err
}

func (m *VMserver) ContainerStatus(ctx context.Context, req *kubeapi.ContainerStatusRequest) (*kubeapi.ContainerStatusResponse, error) {
	glog.Infof("ContainerStatus: req = %+v", req)

	resp, err := m.contProvider.ContainerStatus(req)

	glog.Infof("ContainerStatus: resp = %+v, err = %v", resp, err)

	return resp, err
}

func (m *VMserver) ExecSync(ctx context.Context, req *kubeapi.ExecSyncRequest) (*kubeapi.ExecSyncResponse, error) {
	glog.Infof("ExecSync: req = %+v", req)

	resp, err := m.contProvider.ExecSync(req)

	glog.Infof("ExecSync: resp = %+v, err = %v", resp, err)

	return resp, err
}

func (m *VMserver) UpdateRuntimeConfig(ctx context.Context, req *kubeapi.UpdateRuntimeConfigRequest) (*kubeapi.UpdateRuntimeConfigResponse, error) {
	return nil, errors.New("UpdateRuntimeConfig is currently unsupported")
}

func (m *VMserver) Status(ctx context.Context, req *kubeapi.StatusRequest) (*kubeapi.StatusResponse, error) {
	return nil, errors.New("Status: is currently unsupported")
}

func (m *VMserver) Logs(req *common.LogsRequest, stream common.VMServer_LogsServer) error {
	glog.Infof("Logs: req = %+v", req)

	err := m.contProvider.Logs(req, stream)

	glog.Infof("Logs: err = %v", err)

	return err
}

func (m *VMserver) GetMetrics(ctx context.Context, req *common.GetMetricsRequest) (*common.GetMetricsResponse, error) {
	glog.Infof("GetMetrics: req = %+v", req)

	options := cadvisorapiv2.RequestOptions{
		IdType:    cadvisorapiv2.TypeName,
		Count:     int(req.Count),
		Recursive: true,
	}

	infos, err := m.cadvisor.GetContainerInfoV2("/", options)
	containers := [][]byte{}
	if err == nil {
		for _, info := range infos {
			if _, ok := info.Spec.Labels[types.KubernetesPodNameLabel]; ok {
				container, err := json.Marshal(info)
				if err != nil {
					glog.Infof("GetMetrics: couldn't marshall the json: %v", err)
				} else {
					containers = append(containers, container)
				}
			}
		}
	}

	resp := &common.GetMetricsResponse{JsonMetricResponses: containers}

	glog.Infof("GetMetrics: resp = %+v, err = %v", resp, err)

	return resp, err
}
