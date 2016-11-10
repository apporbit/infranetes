package docker

import (
	"fmt"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

func generateMountBindings(mounts []*kubeapi.Mount) (result []string) {
	// TODO: resolve podHasSELinuxLabel
	for _, m := range mounts {
		bind := fmt.Sprintf("%s:%s", m.GetHostPath(), m.GetContainerPath())
		readOnly := m.GetReadonly()
		if readOnly {
			bind += ":ro"
		}
		if m.GetSelinuxRelabel() {
			if readOnly {
				bind += ",Z"
			} else {
				bind += ":Z"
			}
		}
		result = append(result, bind)
	}
	return
}
