package systemd

import (
	"crypto/sha1"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/coreos/go-systemd/unit"
	"github.com/golang/glog"

	"github.com/sjpotter/infranetes/pkg/vmserver"
	"github.com/sjpotter/infranetes/pkg/vmserver/common"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
	"path/filepath"
	"syscall"
)

type systemdProvider struct {
	contMap map[string]*common.Container
	mapLock sync.Mutex
}

func init() {
	vmserver.ContainerProviders.RegisterProvider("systemd", NewSystemdProvider)
}

func NewSystemdProvider() (vmserver.ContainerProvider, error) {
	glog.Infof("SystemdProvider: starting")
	systemdProvider := &systemdProvider{
		contMap: make(map[string]*common.Container),
	}

	return systemdProvider, nil
}

func createSystemdUnit(name string, cmd string) error {
	myUnit := []*unit.UnitOption{
		{Section: "Unit", Name: "Description", Value: "Infranetes Systemd Unit for " + name},
		{Section: "Service", Name: "ExecStart", Value: cmd},
		{Section: "Service", Name: "Restart", Value: "no"},
		{Section: "Service", Name: "KillMode", Value: "process"},
	}

	reader := unit.Serialize(myUnit)
	outBytes, err := ioutil.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("ioutil.ReadAll failed: %v\n", err)
	}

	ioutil.WriteFile("/run/systemd/system/"+name+".service", outBytes, 0644)

	command := exec.Command("systemctl", "daemon-reload")
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cmd %v failed to run: %v\n", command, output)
	}

	return nil
}

func (p *systemdProvider) CreateContainer(req *kubeapi.CreateContainerRequest) (*kubeapi.CreateContainerResponse, error) {
	p.mapLock.Lock()
	defer p.mapLock.Unlock()

	name := req.Config.Metadata.GetName()

	//1. fetch binary
	url := "http://" + req.Config.GetImage().GetImage()
	glog.Info("CreateContainer: fetching: %v", url)
	resp, err := http.Get(url)
	if err != nil {
		msg := fmt.Sprintf("CreateContainer: http fetch failed: %v", err)
		glog.Info(msg)
		return nil, errors.New(msg)
	}
	defer resp.Body.Close()

	//2. store binary in filename based on url
	h := sha1.New()
	h.Write([]byte(url))
	cmdPath := "/usr/local/bin/" + fmt.Sprintf("%x", h.Sum(nil))

	bytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		msg := fmt.Sprintf("CreateContainer: ReadAll failed: %v", err)
		glog.Info(msg)
		return nil, errors.New(msg)
	}
	err = ioutil.WriteFile(cmdPath, bytes, 0755)
	if err != nil {
		msg := fmt.Sprintf("CreateContainer: WriteFile failed: %v", err)
		glog.Info(msg)
		return nil, errors.New(msg)
	}

	//3. build systemd unit
	cmdSlice := []string{cmdPath}
	cmdSlice = append(cmdSlice, req.Config.Args...)
	cmd := strings.Join(cmdSlice, " ")
	err = createSystemdUnit(name, cmd)
	if err != nil {
		msg := fmt.Sprintf("CreateContainer: createSystemdUnit failed %v", err)
		glog.Info(msg)
		return nil, errors.New(msg)
	}

	//4. bind mount things into place

	for _, mount := range req.Config.Mounts {
		info, _ := os.Stat(*mount.HostPath)
		if info.IsDir() {
			os.MkdirAll(*mount.ContainerPath, 0755)
		} else {
			dir := filepath.Dir(*mount.HostPath)
			os.MkdirAll(dir, 0755)
			os.Create(*mount.ContainerPath)
		}

		err := syscall.Mount(*mount.HostPath, *mount.ContainerPath, "", syscall.MS_BIND, "")
		if err != nil {
			glog.Warningf("CreateContainer: bind mount of %v to %v failed: %v", mount.HostPath, mount.ContainerPath, err)
		}
	}

	//5. generate container data
	id := req.GetPodSandboxId() + ":" + name
	p.contMap[id] = common.NewContainer(&id,
		req.PodSandboxId,
		kubeapi.ContainerState_CREATED,
		req.Config.Metadata,
		req.Config.Image,
		req.Config.Mounts,
		req.Config.Labels,
		req.Config.Annotations)

	return &kubeapi.CreateContainerResponse{ContainerId: &id}, nil
}

func (p *systemdProvider) StartContainer(req *kubeapi.StartContainerRequest) (*kubeapi.StartContainerResponse, error) {
	p.mapLock.Lock()
	defer p.mapLock.Unlock()

	splits := strings.Split(req.GetContainerId(), ":")
	name := splits[1]

	id := req.GetContainerId()
	if cont, ok := p.contMap[id]; !ok {
		return nil, fmt.Errorf("StartContainer: Invalid ContainerID: %v", id)
	} else {
		command := exec.Command("systemctl", "start", name)
		output, err := command.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("cmd %v failed to run: %v\n", command, output)
		}

		cont.Start()
		return &kubeapi.StartContainerResponse{}, nil
	}
}

func (p *systemdProvider) StopContainer(req *kubeapi.StopContainerRequest) (*kubeapi.StopContainerResponse, error) {
	p.mapLock.Lock()
	defer p.mapLock.Unlock()

	splits := strings.Split(req.GetContainerId(), ":")
	name := splits[1]

	id := req.GetContainerId()
	if cont, ok := p.contMap[id]; !ok {
		return nil, fmt.Errorf("StopContainer: Invalid ContainerID: %v", id)
	} else {
		command := exec.Command("systemctl", "stop", name)
		output, err := command.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("cmd %v failed to run: %v\n", command, output)
		}

		cont.Finished()
		return &kubeapi.StopContainerResponse{}, nil
	}
}

func (p *systemdProvider) RemoveContainer(req *kubeapi.RemoveContainerRequest) (*kubeapi.RemoveContainerResponse, error) {
	p.mapLock.Lock()
	defer p.mapLock.Unlock()

	id := req.GetContainerId()
	if _, ok := p.contMap[id]; !ok {
		return nil, fmt.Errorf("RemoveContainer: Invalid ContainerID: %v", id)
	} else {
		delete(p.contMap, id)
		return &kubeapi.RemoveContainerResponse{}, nil
	}
}

func (p *systemdProvider) ListContainers(req *kubeapi.ListContainersRequest) (*kubeapi.ListContainersResponse, error) {
	p.mapLock.Lock()
	defer p.mapLock.Unlock()

	containers := []*kubeapi.Container{}

	for _, cont := range p.contMap {
		if filter(req.Filter, cont) {
			continue
		}
		containers = append(containers, cont.ToKubeContainer())
	}

	resp := &kubeapi.ListContainersResponse{
		Containers: containers,
	}

	return resp, nil
}

func filter(filter *kubeapi.ContainerFilter, cont *common.Container) bool {
	if filter != nil {
		if filter.GetId() != "" && filter.GetId() == *cont.GetId() {
			glog.Infof("Filtering out %v as want %v", *cont.GetId(), filter.GetId())
			return true
		}

		if filter.GetState() == cont.GetState() {
			glog.Infof("Filtering out %v as want %v and got %v", *cont.GetId(), filter.GetState(), cont.GetState())
			return true
		}

		if filter.GetPodSandboxId() != "" && filter.GetPodSandboxId() != *cont.GetPodId() {
			glog.Infof("Filtering out %v as want %v and got %v", *cont.GetId(), filter.GetPodSandboxId(), *cont.GetPodId())
			return true
		}

		for k, v := range filter.GetLabelSelector() {
			if podVal, ok := cont.GetLabels()[k]; !ok {
				glog.Infof("didn't find key %v in local labels: %+v", k, cont.GetLabels())
			} else {
				if podVal != v {
					glog.Infof("Filtering out %v as want labels[%v] = %v and got %v", *cont.GetId(), k, v, podVal)
					return true
				}
			}
		}
	}

	return false
}

func (p *systemdProvider) ContainerStatus(req *kubeapi.ContainerStatusRequest) (*kubeapi.ContainerStatusResponse, error) {
	p.mapLock.Lock()
	defer p.mapLock.Unlock()

	id := req.GetContainerId()
	if cont, ok := p.contMap[id]; !ok {
		return nil, fmt.Errorf("ContainerStatus: Invalid ContainerID: %v", id)
	} else {
		resp := &kubeapi.ContainerStatusResponse{
			Status: cont.ToKubeStatus(),
		}

		return resp, nil
	}
}

func (p *systemdProvider) Exec(_ kubeapi.RuntimeService_ExecServer) error {
	return errors.New("unimplemented")
}
