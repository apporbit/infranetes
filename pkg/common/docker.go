/* Helpers functions for docker in both the infranetes cri implementation and the vmserver */

package common

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	dockertypes "github.com/docker/engine-api/types"
	dockerfilters "github.com/docker/engine-api/types/filters"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

func GenerateEnvList(envs []*kubeapi.KeyValue) (result []string) {
	for _, env := range envs {
		result = append(result, fmt.Sprintf("%s=%s", env.GetKey(), env.GetValue()))
	}
	return
}

const (
	annotationPrefix = "annotation."
)

func MakeLabels(labels, annotations map[string]string) map[string]string {
	merged := make(map[string]string)
	for k, v := range labels {
		merged[k] = v
	}
	for k, v := range annotations {
		// Assume there won't be conflict.
		merged[fmt.Sprintf("%s%s", annotationPrefix, k)] = v
	}
	return merged
}

func ToDockerContainerStatus(state kubeapi.ContainerState) string {
	switch state {
	case kubeapi.ContainerState_CONTAINER_CREATED:
		return "created"
	case kubeapi.ContainerState_CONTAINER_RUNNING:
		return "running"
	case kubeapi.ContainerState_CONTAINER_EXITED:
		return "exited"
	case kubeapi.ContainerState_CONTAINER_UNKNOWN:
		fallthrough
	default:
		return "unknown"
	}
}

const (
	// Status of a container returned by docker ListContainers
	statusRunningPrefix = "Up"
	statusCreatedPrefix = "Created"
	statusExitedPrefix  = "Exited"
)

func ToRuntimeAPIContainerState(state string) kubeapi.ContainerState {
	// Parse the state string in dockertypes.Container. This could break when
	// we upgrade docker.
	switch {
	case strings.HasPrefix(state, statusRunningPrefix):
		return kubeapi.ContainerState_CONTAINER_RUNNING
	case strings.HasPrefix(state, statusExitedPrefix):
		return kubeapi.ContainerState_CONTAINER_EXITED
	case strings.HasPrefix(state, statusCreatedPrefix):
		return kubeapi.ContainerState_CONTAINER_CREATED
	default:
		return kubeapi.ContainerState_CONTAINER_UNKNOWN
	}
}

func parseUint32(s string) (uint32, error) {
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, err
	}
	return uint32(n), nil
}

func ToRuntimeAPIContainer(podId string, name string, c *dockertypes.Container) (*kubeapi.Container, error) {
	state := ToRuntimeAPIContainerState(c.Status)

	id := podId + ":" + c.ID

	metadata, err := ParseContainerName(name)
	if err != nil {
		return nil, fmt.Errorf("toRuntimeAPIContainer: unable to parse name label: %q", c.Names[0])
	}

	labels, annotations := ExtractLabels(c.Labels)
	return &kubeapi.Container{
		Id:          &id,
		Metadata:    metadata,
		Image:       &kubeapi.ImageSpec{Image: &c.Image},
		ImageRef:    &c.ImageID,
		State:       &state,
		Labels:      labels,
		Annotations: annotations,
	}, nil
}

var (
	kubePrefix         = "kube"
	nameDelimiter      = "_"
	containerNameLabel = "containerNameLabel"
)

func MakeContainerName(s *kubeapi.PodSandboxConfig, c *kubeapi.ContainerConfig) string {
	sandboxMetadata := &kubeapi.PodSandboxMetadata{}
	if s != nil && s.Metadata != nil {
		sandboxMetadata = s.Metadata
	}
	contMetadata := &kubeapi.ContainerMetadata{}
	if c != nil && c.Metadata != nil {
		contMetadata = c.Metadata
	}
	return strings.Join([]string{
		kubePrefix,                                   // 0
		contMetadata.GetName(),                       // 1:
		sandboxMetadata.GetName(),                    // 2: sandbox name
		sandboxMetadata.GetNamespace(),               // 3: sandbox namesapce
		sandboxMetadata.GetUid(),                     // 4  sandbox uid
		fmt.Sprintf("%d", contMetadata.GetAttempt()), // 5
	}, nameDelimiter)
}

// TODO: Evaluate whether we should rely on labels completely.
func ParseContainerName(name string) (*kubeapi.ContainerMetadata, error) {
	parts := strings.Split(name, nameDelimiter)
	if len(parts) != 6 {
		return nil, fmt.Errorf("failed to parse the container name: %q", name)
	}
	if parts[0] != kubePrefix {
		return nil, fmt.Errorf("container is not managed by kubernetes: %q", name)
	}

	attempt, err := parseUint32(parts[5])
	if err != nil {
		return nil, fmt.Errorf("failed to parse the container name %q: %v", name, err)
	}

	return &kubeapi.ContainerMetadata{
		Name:    &parts[1],
		Attempt: &attempt,
	}, nil
}

// dockerFilter wraps around dockerfilters.Args and provides methods to modify
// the filter easily.
type dockerFilter struct {
	args *dockerfilters.Args
}

func NewDockerFilter(args *dockerfilters.Args) *dockerFilter {
	return &dockerFilter{args: args}
}

func (f *dockerFilter) Add(key, value string) {
	f.args.Add(key, value)
}

func (f *dockerFilter) AddLabel(key, value string) {
	f.Add("label", fmt.Sprintf("%s=%s", key, value))
}

// ParseDockerTimestamp parses the timestamp returned by DockerInterface from string to time.Time
func ParseDockerTimestamp(s string) (time.Time, error) {
	// Timestamp returned by Docker is in time.RFC3339Nano format.
	return time.Parse(time.RFC3339Nano, s)
}

func GetContainerTimestamps(r *dockertypes.ContainerJSON) (time.Time, time.Time, time.Time, error) {
	var createdAt, startedAt, finishedAt time.Time
	var err error

	createdAt, err = ParseDockerTimestamp(r.Created)
	if err != nil {
		return createdAt, startedAt, finishedAt, err
	}
	startedAt, err = ParseDockerTimestamp(r.State.StartedAt)
	if err != nil {
		return createdAt, startedAt, finishedAt, err
	}
	finishedAt, err = ParseDockerTimestamp(r.State.FinishedAt)
	if err != nil {
		return createdAt, startedAt, finishedAt, err
	}
	return createdAt, startedAt, finishedAt, nil
}

func ExtractLabels(input map[string]string) (map[string]string, map[string]string) {
	labels := make(map[string]string)
	annotations := make(map[string]string)
	for k, v := range input {
		// Check if the label should be treated as an annotation.
		if strings.HasPrefix(k, annotationPrefix) {
			annotations[strings.TrimPrefix(k, annotationPrefix)] = v
			continue
		}
		labels[k] = v
	}
	return labels, annotations
}

func ToRuntimeAPIImage(image *dockertypes.Image) (*kubeapi.Image, error) {
	if image == nil {
		return nil, errors.New("unable to convert a nil pointer to a runtime API image")
	}

	size := uint64(image.VirtualSize)
	return &kubeapi.Image{
		Id:          &image.ID,
		RepoTags:    image.RepoTags,
		RepoDigests: image.RepoDigests,
		Size_:       &size,
	}, nil
}
