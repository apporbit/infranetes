package fake

import (
	"net"

	"github.com/apcera/libretto/ssh"
)

type fakeVM struct {
	name string
}

func (v *fakeVM) GetName() string {
	return v.name
}
func (v *fakeVM) Provision() error {
	return nil
}

func (v *fakeVM) GetIPs() ([]net.IP, error) {
	return nil, nil
}

func (v *fakeVM) Destroy() error {
	return nil
}

func (v *fakeVM) GetState() (string, error) {
	return "", nil
}

func (v *fakeVM) Suspend() error {
	return nil
}

func (v *fakeVM) Resume() error {
	return nil
}

func (v *fakeVM) Halt() error {
	return nil
}

func (v *fakeVM) Start() error {
	return nil
}

func (v *fakeVM) GetSSH(ssh.Options) (ssh.Client, error) {
	return nil, nil
}
