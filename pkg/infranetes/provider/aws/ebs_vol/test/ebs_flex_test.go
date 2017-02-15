package test

import (
	"encoding/json"
	"testing"

	fv "k8s.io/kubernetes/pkg/volume/flexvolume"
)

func TestSuccessVar(t *testing.T) {
	_, err := json.Marshal(fv.FlexVolumeDriverStatus{Status: fv.StatusSuccess})

	if err != nil {
		t.Errorf("json.Marshal failed to generate success message")
	}
}
