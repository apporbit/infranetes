package fake

import (
	"fmt"
	"strconv"

	"github.com/sjpotter/infranetes/cmd/infranetes/flags"
	"github.com/sjpotter/infranetes/pkg/infranetes/provider"
	"github.com/sjpotter/infranetes/pkg/infranetes/provider/common"
	"github.com/sjpotter/infranetes/pkg/infranetes/types"
	"github.com/sjpotter/infranetes/pkg/utils"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1"
)

type fakePodProvider struct {
	instances map[string]*common.PodData
	ipList    *utils.Deque
}

func init() {
	provider.ImageProviders.RegisterProvider("fake", NewFakeImagerProvider)
	provider.PodProviders.RegisterProvider("fake", NewFakePodProvider)
}

func NewFakePodProvider() (provider.PodProvider, error) {
	ipList := utils.NewDeque()
	for i := 1; i <= 255; i++ {
		ipList.Append(fmt.Sprint(*flags.IPBase + "." + strconv.Itoa(i)))
	}

	provider := &fakePodProvider{
		instances: make(map[string]*common.PodData),
		ipList:    ipList,
	}

	return provider, nil
}

func (p *fakePodProvider) SetBootAtRun(boot bool) {}

func (p *fakePodProvider) RunPodSandbox(req *kubeapi.RunPodSandboxRequest, voluems []*types.Volume) (*common.PodData, error) {
	name := "fake-" + utils.RandString(10)
	vm := &fakeVM{
		name: name,
	}

	client, _ := common.CreateFakeClient()
	podIp := p.ipList.Shift().(string)
	booted := true
	podData := common.NewPodData(vm, &vm.name, req.Config.Metadata, req.Config.Annotations, req.Config.Labels, podIp, req.Config.Linux, client, booted, nil)

	p.instances[name] = podData

	return podData, nil
}

func (*fakePodProvider) PreCreateContainer(podData *common.PodData, req *kubeapi.CreateContainerRequest, f func(req *kubeapi.ImageStatusRequest) (*kubeapi.ImageStatusResponse, error)) error {
	return nil
}

func (*fakePodProvider) UpdatePodState(cPodData *common.PodData) {}

func (*fakePodProvider) StopPodSandbox(podData *common.PodData) {}

func (v *fakePodProvider) RemovePodSandbox(data *common.PodData) {
	// putting ip back into queue
	v.ipList.Append(data.Ip)
}

func (v *fakePodProvider) PodSandboxStatus(podData *common.PodData) {}

func (v *fakePodProvider) ListInstances() ([]*common.PodData, error) {
	return nil, nil
}
