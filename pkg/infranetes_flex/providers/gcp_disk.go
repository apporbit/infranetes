package infranetes_flex

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"time"

	"github.com/sjpotter/infranetes/pkg/infranetes/provider/gcp"

	"github.com/sjpotter/infranetes/pkg/infranetes_flex"
)

func init() {
	infranetes_flex.DevProviders.RegisterProvider("aws_ebs", NewGCPDiskProvider)
}

func NewGCPDiskProvider() (infranetes_flex.DevProvider, error) {
	var conf gcp.GceConfig
	file, err := ioutil.ReadFile("/root/gce.json")
	if err != nil {
		return nil, fmt.Errorf("File error: %v\n", err)
	}

	json.Unmarshal(file, &conf)

	if conf.SourceImage == "" || conf.Zone == "" || conf.Project == "" || conf.Scope == "" || conf.AuthFile == "" || conf.Network == "" || conf.Subnet == "" {
		return nil, fmt.Errorf("Failed to read in complete config file: conf = %+v", conf)
	}
	return &gcpDiskProvider{
		config: &conf,
	}, nil
}

type gcpDiskProvider struct {
	config *gcp.GceConfig
}

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func RandStringRunes(n int) string {
	rand.Seed(time.Now().UnixNano())

	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}

	return string(b)
}

func (p *gcpDiskProvider) Provision(size uint64) (*string, error) {
	isize := int64(size)
	name := "infranetes-disk-" + RandStringRunes(16)

	s, err := gcp.GetService(p.config.AuthFile, p.config.Project, p.config.Zone, []string{p.config.Scope})
	if err != nil {
		return nil, err
	}

	err = s.CreateDisk(name, isize)

	return &name, err
}

func (p *gcpDiskProvider) Attach(id *string) (*string, error) {
	s, err := gcp.GetService(p.config.AuthFile, p.config.Project, p.config.Zone, []string{p.config.Scope})
	if err != nil {
		return nil, err
	}

	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("hostname failed: %v", err)
	}

	err = s.AttachDisk(*id, hostname, *id)
	dev := "/dev/disk/by-id/google-" + *id

	return &dev, err
}

func (p *gcpDiskProvider) Detach(id *string) error {
	s, err := gcp.GetService(p.config.AuthFile, p.config.Project, p.config.Zone, []string{p.config.Scope})
	if err != nil {
		return err
	}

	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("hostname failed: %v", err)
	}

	return s.DetatchDisk(hostname, *id)
}
