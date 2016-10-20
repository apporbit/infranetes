package docker

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/sjpotter/infranetes/pkg/common"

	dockerclient "github.com/docker/engine-api/client"
	dockertypes "github.com/docker/engine-api/types"
	dockercontainer "github.com/docker/engine-api/types/container"
	dockerfilters "github.com/docker/engine-api/types/filters"
	dockerstrslice "github.com/docker/engine-api/types/strslice"

	"github.com/sjpotter/infranetes/pkg/vmserver"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

type dockerProvider struct {
	client *dockerclient.Client
}

const (
	containerNameLabel = "infra.name-label"
	podSandboxIDLabel  = "infra.sandbox-label"
)

func init() {
	vmserver.ContainerProviders.RegisterProvider("docker", NewDockerProvider)
}

func NewDockerProvider() (vmserver.ContainerProvider, error) {
	if client, err := dockerclient.NewClient(dockerclient.DefaultDockerHost, "", nil, nil); err != nil {
		return nil, err
	} else {
		dockerProvider := &dockerProvider{
			client: client,
		}

		return dockerProvider, nil
	}

}

func (d *dockerProvider) CreateContainer(req *kubeapi.CreateContainerRequest) (*kubeapi.CreateContainerResponse, error) {
	config := req.Config
	podSandboxID := req.GetPodSandboxId()

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
		IpcMode:     "host",
		PidMode:     "host",
		NetworkMode: "host",
		UTSMode:     "host",
	}

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

func (d *dockerProvider) StartContainer(req *kubeapi.StartContainerRequest) (*kubeapi.StartContainerResponse, error) {
	splits := strings.Split(req.GetContainerId(), ":")
	contId := splits[1]

	err := d.client.ContainerStart(context.Background(), contId)
	if err != nil {
		return nil, fmt.Errorf("ContainerStart failed: %v", err)
	}

	resp := &kubeapi.StartContainerResponse{}

	return resp, nil
}

func (d *dockerProvider) StopContainer(req *kubeapi.StopContainerRequest) (*kubeapi.StopContainerResponse, error) {
	splits := strings.Split(req.GetContainerId(), ":")
	contId := splits[1]

	err := d.client.ContainerStop(context.Background(), contId, int(req.GetTimeout()))

	resp := &kubeapi.StopContainerResponse{}

	return resp, err
}

func (d *dockerProvider) RemoveContainer(req *kubeapi.RemoveContainerRequest) (*kubeapi.RemoveContainerResponse, error) {
	splits := strings.Split(req.GetContainerId(), ":")
	contId := splits[1]

	err := d.client.ContainerRemove(context.Background(), contId, dockertypes.ContainerRemoveOptions{RemoveVolumes: true})

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
			splits := strings.Split(req.Filter.GetId(), ":")
			f.Add("id", splits[1])
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

	for i := range containers {
		c := containers[i]

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
	splits := strings.Split(req.GetContainerId(), ":")
	podId := splits[0]
	contId := splits[1]

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
		state = kubeapi.ContainerState_RUNNING
	} else {
		// Container is *not* running. We need to get more details.
		//    * Case 1: container has run and exited with non-zero finishedAt
		//              time.
		//    * Case 2: container has failed to start; it has a zero finishedAt
		//              time, but a non-zero exit code.
		//    * Case 3: container has been created, but not started (yet).
		if !finishedAt.IsZero() { // Case 1
			state = kubeapi.ContainerState_EXITED
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
			state = kubeapi.ContainerState_EXITED
			// Adjust finshedAt and startedAt time to createdAt time to avoid
			// the confusion.
			finishedAt, startedAt = createdAt, createdAt
			reason = "ContainerCannotRun"
		} else { // Case 3
			state = kubeapi.ContainerState_CREATED
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

func (d *dockerProvider) Exec(_ kubeapi.RuntimeService_ExecServer) error {
	return errors.New("unimplemented")
}
