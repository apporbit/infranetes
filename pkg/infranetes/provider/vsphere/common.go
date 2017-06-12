package vsphere

import (
	"github.com/apporbit/infranetes/pkg/common"
)

type vsphereConfig struct {
	Host       string
	Username   string
	Password   string
	Datastore  string
	Datacenter string
	Network    string
	Location   string
	Insecure   bool

	Template string
	Routes   []common.AddRouteRequest
}
