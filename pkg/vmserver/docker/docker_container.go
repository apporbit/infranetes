package docker

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	dockerclient "github.com/docker/engine-api/client"
	dockertypes "github.com/docker/engine-api/types"
	dockercontainer "github.com/docker/engine-api/types/container"
	dockerfilters "github.com/docker/engine-api/types/filters"
	dockerstrslice "github.com/docker/engine-api/types/strslice"
	"github.com/hpcloud/tail"
	"k8s.io/kubernetes/pkg/kubelet/dockertools"

	"github.com/sjpotter/infranetes/pkg/common"
	icommon "github.com/sjpotter/infranetes/pkg/common"
	"github.com/sjpotter/infranetes/pkg/vmserver"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

type dockerProvider struct {
	client           *dockerclient.Client
	streamingRuntime *streamingRuntime
	tailMap          map[string]*tail.Tail
	lock             sync.Mutex
}

const (
	containerNameLabel = "infra.name-label"
	podSandboxIDLabel  = "infra.sandbox-label"
	defaultTimeout     = 2 * time.Minute
)

func init() {
	vmserver.ContainerProviders.RegisterProvider("docker", NewDockerProvider)
}

func NewDockerProvider() (vmserver.ContainerProvider, error) {
	glog.Infof("DockerProvider: starting")

	createMountablePaths()

	if client, err := dockerclient.NewClient(dockerclient.DefaultDockerHost, "", nil, nil); err != nil {
		return nil, err
	} else {
		d := &dockerProvider{
			client:  client,
			tailMap: make(map[string]*tail.Tail),
			streamingRuntime: &streamingRuntime{
				client:      dockertools.KubeWrapDockerclient(client),
				execHandler: &dockertools.NativeExecHandler{},
				//execHandler: &dockertools.NsenterExecHandler{},
			},
		}

		return d, nil
	}
}

func createMountablePaths() {
}

// FIXME: A lot of things to support (ala full linux/security context)
func (d *dockerProvider) CreateContainer(req *kubeapi.CreateContainerRequest) (*kubeapi.CreateContainerResponse, error) {
	config := req.Config
	podSandboxID := req.GetPodSandboxId()

	sharedPaths, err := processSharedPaths(config.Annotations)
	if err != nil {
		return nil, fmt.Errorf("ContainerCreate Failed: %v", err)
	}

	labels := common.MakeLabels(config.Labels, config.Annotations)
	labels[containerNameLabel] = common.MakeContainerName(req.SandboxConfig, req.Config)
	labels[podSandboxIDLabel] = podSandboxID

	// Not needed for Infranetes
	// Apply a the container type label.
	// labels[containerTypeLabelKey] = containerTypeLabelContainer
	// Write the sandbox ID in the labels.
	// labels[sandboxIDLabelKey] = podSandboxID

	image := ""
	if iSpec := config.GetImage(); iSpec != nil {
		image = iSpec.GetImage()
	}

	if image != "" {
		pullresp, err := d.client.ImagePull(context.Background(), image, dockertypes.ImagePullOptions{})
		if err != nil {
			return nil, fmt.Errorf("ImagePull Failed (%v)\n", err)
		}

		decoder := json.NewDecoder(pullresp)
		for {
			var msg interface{}
			err := decoder.Decode(&msg)

			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("Pull Image failed: %v", err)
			}
		}

		pullresp.Close()
	}

	createConfig := &dockercontainer.Config{
		//		Hostname:   req.GetSandboxConfig().GetHostname(),
		Entrypoint: dockerstrslice.StrSlice(config.GetCommand()),
		Cmd:        dockerstrslice.StrSlice(config.GetArgs()),
		Env:        common.GenerateEnvList(config.GetEnvs()),
		Image:      image,
		WorkingDir: config.GetWorkingDir(),
		// Interactive containers:
		OpenStdin: config.GetStdin(),
		StdinOnce: config.GetStdinOnce(),
		Tty:       config.GetTty(),
		Labels:    labels,
	}

	hostConfig := &dockercontainer.HostConfig{
		Binds:       generateMountBindings(config.GetMounts(), sharedPaths),
		IpcMode:     "host",
		PidMode:     "host",
		NetworkMode: "host",
		UTSMode:     "host",
	}
	if req.SandboxConfig.DnsConfig != nil {
		hostConfig.DNS = req.SandboxConfig.DnsConfig.Servers
		hostConfig.DNSOptions = req.SandboxConfig.DnsConfig.Options
		hostConfig.DNSSearch = req.SandboxConfig.DnsConfig.Searches
	}

	if req.SandboxConfig.GetLinux().GetSecurityContext().GetPrivileged() {
		hostConfig.Privileged = true
	}

	devices := make([]dockercontainer.DeviceMapping, len(config.Devices))
	for i, device := range config.Devices {
		devices[i] = dockercontainer.DeviceMapping{
			PathOnHost:        device.GetHostPath(),
			PathInContainer:   device.GetContainerPath(),
			CgroupPermissions: device.GetPermissions(),
		}
	}
	hostConfig.Resources.Devices = devices

	dockResp, err := d.client.ContainerCreate(context.Background(), createConfig, hostConfig, nil, "")
	if err != nil {
		return nil, fmt.Errorf("ContainerCreate Failed: %v", err)
	}

	id := podSandboxID + ":" + dockResp.ID

	resp := &kubeapi.CreateContainerResponse{
		ContainerId: &id,
	}

	return resp, nil
}

func processSharedPaths(annotations map[string]string) (map[string]bool, error) {
	ret := make(map[string]bool)
	pathsString, ok := annotations["infranetes.sharedpaths"]
	if !ok {
		return ret, nil
	}

	paths := strings.Split(pathsString, ",")
	for _, path := range paths {
		os.MkdirAll(path, 0755)
		err := syscall.Mount(path, path, "", syscall.MS_BIND|syscall.MS_MGC_VAL, "")
		if err != nil {
			msg := fmt.Sprintf("Failed to bind mount %v: %v", path, err)
			glog.Error(msg)
			return nil, errors.New(msg)
		} else {
			err := syscall.Mount("none", path, "", syscall.MS_REC|syscall.MS_SHARED, "")
			if err != nil {
				msg := fmt.Sprintf("failed to make path %v shared: %v", path, err)
				glog.Error(msg)
				return nil, errors.New(msg)
			}

			ret[path] = true
		}
	}

	return ret, nil
}

func (d *dockerProvider) StartContainer(req *kubeapi.StartContainerRequest) (*kubeapi.StartContainerResponse, error) {
	_, contId, err := icommon.ParseContainer(req.GetContainerId())
	if err != nil {
		return nil, fmt.Errorf("StartContainer: err = %v", err)
	}

	err = d.client.ContainerStart(context.Background(), contId)
	if err != nil {
		return nil, fmt.Errorf("ContainerStart failed: %v", err)
	}

	resp := &kubeapi.StartContainerResponse{}

	return resp, nil
}

func (d *dockerProvider) StopContainer(req *kubeapi.StopContainerRequest) (*kubeapi.StopContainerResponse, error) {
	_, contId, err := icommon.ParseContainer(req.GetContainerId())
	if err != nil {
		return nil, fmt.Errorf("StopContainer: err = %v", err)
	}

	err = d.client.ContainerStop(context.Background(), contId, int(req.GetTimeout()))

	resp := &kubeapi.StopContainerResponse{}

	return resp, err
}

func (d *dockerProvider) RemoveContainer(req *kubeapi.RemoveContainerRequest) (*kubeapi.RemoveContainerResponse, error) {
	_, contId, err := icommon.ParseContainer(req.GetContainerId())
	if err != nil {
		return nil, fmt.Errorf("RemoveContainer: err = %v", err)
	}

	t, err := d.getTail(contId)
	if err == nil {
		t.Stop()
	}

	err = d.client.ContainerRemove(context.Background(), contId, dockertypes.ContainerRemoveOptions{RemoveVolumes: true})

	resp := &kubeapi.RemoveContainerResponse{}

	return resp, err
}

func (d *dockerProvider) ListContainers(req *kubeapi.ListContainersRequest) (*kubeapi.ListContainersResponse, error) {
	opts := dockertypes.ContainerListOptions{All: true}
	opts.Filter = dockerfilters.NewArgs()

	f := common.NewDockerFilter(&opts.Filter)

	// Not Needed for Infranetes
	//f.AddLabel(containerTypeLabelKey, containerTypeLabelContainer)

	if req.Filter != nil {
		if req.Filter.Id != nil {
			_, contId, err := icommon.ParseContainer(req.GetFilter().GetId())
			if err != nil {
				return nil, fmt.Errorf("ListContainers: err = %v", err)
			}

			f.Add("id", contId)
		}

		if req.Filter.State != nil {
			opts.Filter.Add("status", common.ToDockerContainerStatus(req.Filter.GetState()))
		}

		if req.GetFilter().LabelSelector != nil {
			for k, v := range req.Filter.LabelSelector {
				f.AddLabel(k, v)
			}
		}
	}

	result := []*kubeapi.Container{}

	containers, err := d.client.ContainerList(context.Background(), opts)
	if err != nil {
		glog.Infof("ListContainers: docker client returned an error: %v", err)
		return nil, err
	}

	for _, c := range containers {
		name, ok := c.Labels[containerNameLabel]
		if !ok {
			glog.Infof("ContainerStatus: couldn't find container name for %v", c.ID)
			continue
		}
		podId, ok := c.Labels[podSandboxIDLabel]
		if !ok {
			glog.Infof("ContainerStatus: couldn't find podId for %v", c.ID)
			continue
		}

		converted, err := common.ToRuntimeAPIContainer(podId, name, &c)
		if err != nil {
			glog.Infof("ListContainers: Unable to convert docker to runtime API container: %v", err)
			continue
		}

		result = append(result, converted)
	}

	resp := &kubeapi.ListContainersResponse{
		Containers: result,
	}

	return resp, nil
}

func (d *dockerProvider) ContainerStatus(req *kubeapi.ContainerStatusRequest) (*kubeapi.ContainerStatusResponse, error) {
	podId, contId, err := icommon.ParseContainer(req.GetContainerId())
	if err != nil {
		return nil, fmt.Errorf("ContainerStatus: err = %v", err)
	}

	r, err := d.client.ContainerInspect(context.Background(), contId)
	if err != nil {
		return nil, err
	}

	// Parse the timstamps.
	createdAt, startedAt, finishedAt, err := common.GetContainerTimestamps(&r)
	if err != nil {
		return nil, fmt.Errorf("failed to parse timestamp for container %q: %v", contId, err)
	}

	// Convert the mounts.
	mounts := []*kubeapi.Mount{}
	for _, m := range r.Mounts {
		readonly := !m.RW
		mounts = append(mounts, &kubeapi.Mount{
			ContainerPath: &m.Destination,
			HostPath:      &m.Source,
			Readonly:      &readonly,
			// Note: Can't set SeLinuxRelabel
		})
	}
	// Interpret container states.
	var state kubeapi.ContainerState
	var reason string
	if r.State.Running {
		// Container is running.
		state = kubeapi.ContainerState_CONTAINER_RUNNING
	} else {
		// Container is *not* running. We need to get more details.
		//    * Case 1: container has run and exited with non-zero finishedAt
		//              time.
		//    * Case 2: container has failed to start; it has a zero finishedAt
		//              time, but a non-zero exit code.
		//    * Case 3: container has been created, but not started (yet).
		if !finishedAt.IsZero() { // Case 1
			state = kubeapi.ContainerState_CONTAINER_EXITED
			switch {
			case r.State.OOMKilled:
				// Note: if an application handles OOMKilled gracefully, the
				// exit code could be zero.
				reason = "OOMKilled"
			case r.State.ExitCode == 0:
				reason = "Completed"
			default:
				reason = fmt.Sprintf("Error: %s", r.State.Error)
			}
		} else if !finishedAt.IsZero() && r.State.ExitCode != 0 { // Case 2
			state = kubeapi.ContainerState_CONTAINER_EXITED
			// Adjust finshedAt and startedAt time to createdAt time to avoid
			// the confusion.
			finishedAt, startedAt = createdAt, createdAt
			reason = "ContainerCannotRun"
		} else { // Case 3
			state = kubeapi.ContainerState_CONTAINER_CREATED
		}
	}

	// Convert to unix timestamps.
	ct, st, ft := createdAt.Unix(), startedAt.Unix(), finishedAt.Unix()
	exitCode := int32(r.State.ExitCode)

	name, ok := r.Config.Labels[containerNameLabel]
	if !ok {
		glog.Infof("ContainerStatus: couldn't find container name for %v", req.ContainerId)
		return nil, fmt.Errorf("ContainerStatus: couldn't find container name for %v", req.ContainerId)
	}

	metadata, err := common.ParseContainerName(name)
	if err != nil {
		return nil, err
	}

	id := podId + ":" + r.ID

	labels, annotations := common.ExtractLabels(r.Config.Labels)
	resp := &kubeapi.ContainerStatusResponse{
		Status: &kubeapi.ContainerStatus{
			Id:          &id,
			Metadata:    metadata,
			Image:       &kubeapi.ImageSpec{Image: &r.Config.Image},
			ImageRef:    &r.Image,
			Mounts:      mounts,
			ExitCode:    &exitCode,
			State:       &state,
			CreatedAt:   &ct,
			StartedAt:   &st,
			FinishedAt:  &ft,
			Reason:      &reason,
			Labels:      labels,
			Annotations: annotations,
		},
	}

	return resp, nil
}

func (d *dockerProvider) setTail(cont string, tail *tail.Tail) {
	d.lock.Lock()
	defer d.lock.Unlock()

	d.tailMap[cont] = tail
}

func (d *dockerProvider) getTail(cont string) (*tail.Tail, error) {
	d.lock.Lock()
	defer d.lock.Unlock()

	tail, ok := d.tailMap[cont]
	if !ok {
		return nil, fmt.Errorf("getTail: %v doesn't exist", cont)
	}

	return tail, nil
}

func getTimeoutContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), defaultTimeout)
}

func getCancelableContext() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}
