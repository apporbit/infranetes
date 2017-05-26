package infranetes

import (
	"fmt"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/golang/glog"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	icommon "github.com/sjpotter/infranetes/pkg/common"
	"github.com/sjpotter/infranetes/pkg/infranetes/provider"
	"github.com/sjpotter/infranetes/pkg/infranetes/provider/common"
	"github.com/sjpotter/infranetes/pkg/infranetes/types"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1"
)

var (
	runtimeAPIVersion = "0.1.0"
	supportedFSTypes  = map[string]bool{"nfs4": true}
)

type Manager struct {
	server       *grpc.Server
	podProvider  provider.PodProvider
	contProvider provider.ImageProvider

	vmMap     map[string]*common.PodData //maps internal pod sandbox id to PodData
	vmMapLock sync.RWMutex

	mountMap     map[string]string
	mountMapLock sync.Mutex
	volumeMap    map[string][]*types.Volume
}

func NewInfranetesManager(podProvider provider.PodProvider, contProvider provider.ImageProvider) (*Manager, error) {
	manager := &Manager{
		server:       grpc.NewServer(),
		podProvider:  podProvider,
		contProvider: contProvider,
		vmMap:        make(map[string]*common.PodData),
		volumeMap:    make(map[string][]*types.Volume),
		mountMap:     make(map[string]string),
	}

	manager.importSandboxes()

	manager.registerServer()

	return manager, nil
}

func (s *Manager) Serve(addr string) error {
	glog.V(1).Infof("Start infranetes at %s", addr)

	if err := syscall.Unlink(addr); err != nil && !os.IsNotExist(err) {
		return err
	}

	lis, err := net.Listen("unix", addr)

	if err != nil {
		glog.Fatalf("Failed to listen %s: %v", addr, err)
		return err
	}

	defer lis.Close()
	return s.server.Serve(lis)
}

func (s *Manager) registerServer() {
	kubeapi.RegisterRuntimeServiceServer(s.server, s)
	kubeapi.RegisterImageServiceServer(s.server, s)
	icommon.RegisterMetricsServer(s.server, s)
	icommon.RegisterMountsServer(s.server, s)

}

func (s *Manager) Version(ctx context.Context, req *kubeapi.VersionRequest) (*kubeapi.VersionResponse, error) {
	runtimeName := "infranetes"

	resp := &kubeapi.VersionResponse{
		RuntimeApiVersion: runtimeAPIVersion,
		RuntimeName:       runtimeName,
		RuntimeVersion:    runtimeAPIVersion,
		Version:           runtimeAPIVersion,
	}

	return resp, nil
}

func (m *Manager) RunPodSandbox(ctx context.Context, req *kubeapi.RunPodSandboxRequest) (*kubeapi.RunPodSandboxResponse, error) {
	cookie := rand.Int()
	glog.Infof("%d: RunPodSandbox: req = %+v", cookie, req)
	vcpu, err := common.GetCpuLimitFromCgroup(req.GetConfig().GetLinux().GetCgroupParent())
	if err != nil {
		glog.Infof("Couldn't parse cpu limits: %v", err)
	} else {
		glog.Infof("CPU Limit = %v", vcpu)
	}

	mem, err := common.GetMemeoryLimitFromCgroup(req.GetConfig().GetLinux().GetCgroupParent())
	if err != nil {
		glog.Infof("Couldn't parse mem limits: %v", err)
	} else {
		glog.Infof("MEM Limit = %v", mem)
	}

	resp, err := m.createSandbox(req)

	glog.Infof("%d: RunPodSandbox: resp = %+v, err = %v", cookie, resp, err)

	return resp, err
}

func (m *Manager) StopPodSandbox(ctx context.Context, req *kubeapi.StopPodSandboxRequest) (*kubeapi.StopPodSandboxResponse, error) {
	cookie := rand.Int()
	glog.Infof("%d: StopPodSandbox: req = %+v", cookie, req)

	resp, err := m.stopSandbox(req)

	glog.Infof("%d: StopPodSandbox: resp = %+v, err = %v", cookie, resp, err)

	return resp, err
}

func (m *Manager) RemovePodSandbox(ctx context.Context, req *kubeapi.RemovePodSandboxRequest) (*kubeapi.RemovePodSandboxResponse, error) {
	cookie := rand.Int()
	glog.Infof("%d: RemovePodSandbox: req = %+v", cookie, req)

	err := m.removePodSandbox(req)

	resp := &kubeapi.RemovePodSandboxResponse{}

	glog.Infof("%d: RemovePodSandbox: resp = %+v, err = %v", cookie, resp, err)

	return resp, err
}

func (m *Manager) PodSandboxStatus(ctx context.Context, req *kubeapi.PodSandboxStatusRequest) (*kubeapi.PodSandboxStatusResponse, error) {
	cookie := rand.Int()
	glog.Infof("%d: PodSandboxStatus: req = %+v", cookie, req)

	resp, err := m.podSandboxStatus(req)

	glog.Infof("%d: PodSandboxStatus: resp = %+v, err = %v", cookie, resp, err)

	return resp, err
}

func (m *Manager) ListPodSandbox(ctx context.Context, req *kubeapi.ListPodSandboxRequest) (*kubeapi.ListPodSandboxResponse, error) {
	cookie := rand.Int()
	glog.V(1).Infof("%d: ListPodSandbox: req = %+v", cookie, req)

	resp, err := m.listPodSandbox(req)

	glog.V(1).Infof("%d: ListPodSandbox: resp = %+v, err = %v", cookie, resp, nil)

	return resp, err
}

func (m *Manager) CreateContainer(ctx context.Context, req *kubeapi.CreateContainerRequest) (*kubeapi.CreateContainerResponse, error) {
	glog.Infof("CreateContainer: req = %+v", req)

	podId := req.GetPodSandboxId()

	podData, err := m.getPodData(podId)
	if err != nil {
		glog.Infof("createContainer: failed to get podData for sandbox %v", podId)
		return nil, fmt.Errorf("Failed to get client for sandbox %v: %v", podId, err)
	}

	logpath := filepath.Join(req.GetSandboxConfig().GetLogDirectory(), req.GetConfig().GetLogPath())

	resp, err := m.createContainer(podData, req)

	podData.AddContLogPath(resp.GetContainerId(), logpath)

	glog.Infof("CreateContainer: resp = %+v, err = %v", resp, err)

	return resp, err
}

func isReadOnly(opts string) bool {
	ret := false

	splits := strings.Split(opts, ",")
	for _, split := range splits {
		if split == "ro" {
			ret = true
		}
	}

	return ret
}

func (m *Manager) StartContainer(ctx context.Context, req *kubeapi.StartContainerRequest) (*kubeapi.StartContainerResponse, error) {
	cookie := rand.Int()
	glog.Infof("%d: StartContainer: req = %+v", cookie, req)

	podId, contId, err := icommon.ParseContainer(req.GetContainerId())
	if err != nil {
		return nil, fmt.Errorf("StartContainer: failed: %v", err)
	}

	podData, err := m.getPodData(podId)
	if err != nil {
		glog.Infof("%d: StartContainer: failed to get podData for sandbox %v", cookie, podId)
		return nil, fmt.Errorf("Failed to get podData for sandbox %v: %v", podId, err)
	}

	podData.RLock()
	defer podData.RUnlock()

	client := podData.Client
	if client == nil {
		return nil, fmt.Errorf("CreateContainer: nil client, must be a removed pod sandbox?")
	}

	resp, err := client.StartContainer(req)
	if err == nil { // start worked, start logging
		go func() {
			path, ok := podData.GetContLogPath(req.GetContainerId())
			if !ok {
				glog.Infof("StartContainer: Can't log, couldn't find path for %v", req.GetContainerId())
				return
			}

			client.SaveLogs(contId, path)
		}()
	}

	glog.Infof("%d: StartContainer: resp = %+v, err = %v", cookie, resp, err)

	return resp, err
}

func (m *Manager) StopContainer(ctx context.Context, req *kubeapi.StopContainerRequest) (*kubeapi.StopContainerResponse, error) {
	cookie := rand.Int()
	glog.Infof("%d: StopContainer: req = %+v", cookie, req)

	podId, _, err := icommon.ParseContainer(req.GetContainerId())
	if err != nil {
		return nil, fmt.Errorf("StopContainer: failed: %v", err)
	}

	podData, err := m.getPodData(podId)
	if err != nil {
		glog.Infof("%d: StopContainer: failed to get podData for sandbox %v", cookie, podId)
		return nil, fmt.Errorf("Failed to get podData for sandbox %v: %v", podId, err)
	}

	podData.RLock()
	defer podData.RUnlock()

	client := podData.Client
	if client == nil {
		return nil, fmt.Errorf("CreateContainer: nil client, must be a removed pod sandbox?")
	}

	resp, err := client.StopContainer(req)

	glog.Infof("%d: StopContainer: resp = %+v, err = %v", cookie, resp, err)

	return resp, err
}

func (m *Manager) RemoveContainer(ctx context.Context, req *kubeapi.RemoveContainerRequest) (*kubeapi.RemoveContainerResponse, error) {
	cookie := rand.Int()
	glog.Infof("%d: RemoveContainer: req = %+v", cookie, req)

	podId, _, err := icommon.ParseContainer(req.GetContainerId())
	if err != nil {
		return nil, fmt.Errorf("RemoveContainer: failed: %v", err)
	}

	podData, err := m.getPodData(podId)
	if err != nil {
		glog.Infof("%d: RemoveContainer: failed to get podData for sandbox %v", cookie, podId)
		return nil, fmt.Errorf("Failed to get podData for sandbox %v: %v", podId, err)
	}

	podData.RLock()
	defer podData.RUnlock()

	client := podData.Client
	if client == nil {
		return nil, fmt.Errorf("CreateContainer: nil client, must be a removed pod sandbox?")
	}

	resp, err := client.RemoveContainer(req)

	glog.Infof("%d: RemoveContainer: resp = %+v, err = %v", cookie, resp, err)

	return resp, err
}

func (m *Manager) ListContainers(ctx context.Context, req *kubeapi.ListContainersRequest) (*kubeapi.ListContainersResponse, error) {
	cookie := rand.Int()
	glog.V(1).Infof("%d: ListContainers: req = %+v", cookie, req)

	resp, err := m.listContainers(req)

	glog.V(1).Infof("%d: ListContainers: resp = %+v, err = %v", cookie, resp, err)

	return resp, err
}

func (m *Manager) ContainerStatus(ctx context.Context, req *kubeapi.ContainerStatusRequest) (*kubeapi.ContainerStatusResponse, error) {
	cookie := rand.Int()
	glog.Infof("%d: ContainerStatus: req = %+v", cookie, req)

	podId, _, err := icommon.ParseContainer(req.GetContainerId())
	if err != nil {
		return nil, fmt.Errorf("ContainerStatus: failed: %v", err)
	}

	podData, err := m.getPodData(podId)
	if err != nil {
		glog.Infof("%d: ContainerStatus: failed to get podData for sandbox %v", cookie, podId)
		return nil, fmt.Errorf("failed to get podData for sandbox %v", podId)
	}

	podData.RLock()
	defer podData.RUnlock()

	client := podData.Client
	if client == nil {
		return nil, fmt.Errorf("CreateContainer: nil client, must be a removed pod sandbox?")
	}

	resp, err := client.ContainerStatus(req)

	glog.Infof("%d: ContainerStatus: resp = %+v, err = %v", cookie, resp, err)

	return resp, err
}

func (m *Manager) ExecSync(ctx context.Context, req *kubeapi.ExecSyncRequest) (*kubeapi.ExecSyncResponse, error) {
	cookie := rand.Int()
	glog.Infof("%d: ExecSync: req = %+v", cookie, req)

	splits := strings.Split(req.GetContainerId(), ":")
	podId := splits[0]

	podData, err := m.getPodData(podId)
	if err != nil {
		glog.Infof("%d: ExecSync: failed to get podData for sandbox %v", cookie, podId)
		return nil, fmt.Errorf("failed to get podData for sandbox %v", podId)
	}

	podData.RLock()
	defer podData.RUnlock()

	client := podData.Client
	if client == nil {
		return nil, fmt.Errorf("ExecSync: nil client, must be a removed pod sandbox?")
	}

	resp, err := client.ExecSync(req)

	glog.Infof("%d: ExecSync: resp = %+v, err = %v", cookie, resp, err)

	return resp, err
}

func (m *Manager) Exec(ctx context.Context, req *kubeapi.ExecRequest) (*kubeapi.ExecResponse, error) {
	glog.Infof("Exec: req = %+v", req)

	podId, _, err := icommon.ParseContainer(req.GetContainerId())
	if err != nil {
		return nil, fmt.Errorf("Exec: failed: %v", err)
	}

	podData, err := m.getPodData(podId)
	if err != nil {
		glog.Infof("Exec: failed to get podData for sandbox %v", podId)
		return nil, fmt.Errorf("failed to get podData for sandbox %v", podId)
	}

	podData.RLock()
	defer podData.RUnlock()

	client := podData.Client
	if client == nil {
		return nil, fmt.Errorf("Exec: nil client, must be a removed pod sandbox?")
	}

	resp, err := client.Exec(req)

	glog.Infof("Exec: resp = %+v, err = %v", resp, err)

	return resp, err
}

func (m *Manager) Attach(ctx context.Context, req *kubeapi.AttachRequest) (*kubeapi.AttachResponse, error) {
	glog.Infof("Attach: req = %+v", req)

	podId, _, err := icommon.ParseContainer(req.GetContainerId())
	if err != nil {
		return nil, fmt.Errorf("Attach: failed: %v", err)
	}

	podData, err := m.getPodData(podId)
	if err != nil {
		glog.Infof("Attach: failed to get podData for sandbox %v", podId)
		return nil, fmt.Errorf("failed to get podData for sandbox %v", podId)
	}

	podData.RLock()
	defer podData.RUnlock()

	client := podData.Client
	if client == nil {
		return nil, fmt.Errorf("Attach: nil client, must be a removed pod sandbox?")
	}

	resp, err := client.Attach(req)

	glog.Infof("Attach: resp = %+v, err = %v", resp, err)

	return resp, err
}

func (m *Manager) PortForward(ctx context.Context, req *kubeapi.PortForwardRequest) (*kubeapi.PortForwardResponse, error) {
	glog.Infof("PortForward: req = %+v", req)

	podId := req.GetPodSandboxId()

	podData, err := m.getPodData(podId)
	if err != nil {
		glog.Infof("PortForward: failed to get podData for sandbox %v", podId)
		return nil, fmt.Errorf("failed to get podData for sandbox %v", podId)
	}

	podData.RLock()
	defer podData.RUnlock()

	client := podData.Client
	if client == nil {
		return nil, fmt.Errorf("PortForward: nil client, must be a removed pod sandbox?")
	}

	resp, err := client.PortForward(req)

	glog.Infof("Attach: resp = %+v, err = %v", resp, err)

	return resp, err

}

// TODO: Currently only handles PodCIDR and unsure how that impacts infranetes?  Seems machine specific, but we ignore the machine CIDR
func (m *Manager) UpdateRuntimeConfig(ctx context.Context, req *kubeapi.UpdateRuntimeConfigRequest) (*kubeapi.UpdateRuntimeConfigResponse, error) {
	glog.Infof("UpdateRuntimeConfig: req = %+v", req)

	resp := &kubeapi.UpdateRuntimeConfigResponse{}

	glog.Infof("UpdateRuntimeConfig: resp = %+v, err = %v", resp, nil)

	return resp, nil
}

func (m *Manager) Status(ctx context.Context, req *kubeapi.StatusRequest) (*kubeapi.StatusResponse, error) {
	runtimeReady := &kubeapi.RuntimeCondition{
		Type:   kubeapi.RuntimeReady,
		Status: true,
	}
	networkReady := &kubeapi.RuntimeCondition{
		Type:   kubeapi.NetworkReady,
		Status: true,
	}
	conditions := []*kubeapi.RuntimeCondition{runtimeReady, networkReady}
	status := &kubeapi.RuntimeStatus{Conditions: conditions}

	return &kubeapi.StatusResponse{Status: status}, nil

}

func (m *Manager) ListImages(ctx context.Context, req *kubeapi.ListImagesRequest) (*kubeapi.ListImagesResponse, error) {
	//	glog.Infof("ListImages: req = %+v", req)

	resp, err := m.contProvider.ListImages(req)

	//	glog.Infof("ListImages: resp = %+v, err = %v", resp, err)

	return resp, err
}

func (m *Manager) ImageStatus(ctx context.Context, req *kubeapi.ImageStatusRequest) (*kubeapi.ImageStatusResponse, error) {
	glog.Infof("ImageStatus: req = %+v", req)

	resp, err := m.contProvider.ImageStatus(req)

	glog.Infof("ImageStatus: resp = %+v, err = %v", resp, err)

	return resp, err
}

func (m *Manager) PullImage(ctx context.Context, req *kubeapi.PullImageRequest) (*kubeapi.PullImageResponse, error) {
	glog.Infof("PullImage: req = %+v", req)

	resp, err := m.contProvider.PullImage(req)

	glog.Infof("PullImage: resp = %+v, err = %v", resp, err)

	return resp, err
}

func (m *Manager) RemoveImage(ctx context.Context, req *kubeapi.RemoveImageRequest) (*kubeapi.RemoveImageResponse, error) {
	glog.Infof("RemoveImage: req = %+v", req)

	resp, err := m.contProvider.RemoveImage(req)

	glog.Infof("RemoveImage: resp = %+v, err = %v", resp, err)

	return resp, err
}

// ImageFsInfo returns information of the filesystem that is used to store images.
func (m *Manager) ImageFsInfo(ctx context.Context, req *kubeapi.ImageFsInfoRequest) (*kubeapi.ImageFsInfoResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *Manager) GetMetrics(ctx context.Context, req *icommon.GetMetricsRequest) (*icommon.GetMetricsResponse, error) {
	glog.Infof("GetMetrics: req = %+v", req)

	containers := [][]byte{}

	for _, podData := range m.copyVMMap() {
		resp, err := podData.Client.GetMetric(req)
		if err == nil {
			containers = append(containers, resp.JsonMetricResponses...)
		} else {
			glog.Warningf("Couldn't get metrics for %v: %v", *podData.Id, err)
		}
	}

	resp := &icommon.GetMetricsResponse{JsonMetricResponses: containers}

	glog.Infof("GetMetrics: len of containers slice = %v", len(containers))

	return resp, nil
}

func (m *Manager) AddMount(ctx context.Context, req *icommon.AddMountRequest) (*icommon.AddMountResponse, error) {
	m.mountMapLock.Lock()
	defer m.mountMapLock.Unlock()

	// FIXME: this block should eventually be removed
	if req.MountPoint != "" {
		if _, ok := m.mountMap[req.MountPoint]; ok {
			return nil, fmt.Errorf("AddMount: Already added a mountpoint for %v", req.MountPoint)
		}

		m.mountMap[req.MountPoint] = req.Volume
	}

	vol := &types.Volume{
		Volume:     req.Volume,
		MountPoint: req.MountPoint,
		FsType:     req.FsType,
		ReadOnly:   req.ReadOnly,
		Device:     req.Device,
	}

	m.volumeMap[req.PodUUID] = append(m.volumeMap[req.PodUUID], vol)

	return &icommon.AddMountResponse{}, nil
}

func (m *Manager) DelMount(ctx context.Context, req *icommon.DelMountRequest) (*icommon.DelMountResponse, error) {
	m.mountMapLock.Lock()
	defer m.mountMapLock.Unlock()

	if _, ok := m.mountMap[req.MountPoint]; !ok {
		return nil, fmt.Errorf("DelMount: %v doesn't exist as known mount point", req.MountPoint)
	}

	delete(m.mountMap, req.MountPoint)

	return &icommon.DelMountResponse{}, nil
}
