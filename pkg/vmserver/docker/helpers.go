package docker

import (
	"fmt"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1"
)

func generateMountBindings(mounts []*kubeapi.Mount, sharedPaths map[string]bool) (result []string) {
	// TODO: resolve podHasSELinuxLabel
	for _, m := range mounts {
		bind := fmt.Sprintf("%s:%s", m.GetHostPath(), m.GetContainerPath())

		readOnly := m.GetReadonly()
		shared, _ := sharedPaths[m.GetHostPath()]

		opts := ""

		if readOnly {
			opts += ":ro"
		}

		if m.GetSelinuxRelabel() {
			if opts == "" {
				bind += ":Z"
			} else {
				bind += ",Z"
			}
		}

		if shared {
			if opts == "" {
				bind += ":rshared"
			} else {
				bind += ",rshared"
			}
		}

		result = append(result, bind)
	}
	return
}
