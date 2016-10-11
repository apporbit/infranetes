/* Storing all global flags here for now to avoid circular imports */

package flags

import (
	"flag"
)

var (
	Version      = flag.Bool("version", false, "Print version and exit")
	Listen       = flag.String("listen", "/var/run/infra.sock", "The listen socket, e.g. /var/run/infra.sock")
	ConfigFile   = flag.String("config", "", "Configuration file")
	PodProvider  = flag.String("podprovider", "virtualbox", "Pod Provider to use")
	ContProvider = flag.String("contprovider", "docker", "Container Provider to use")
	CA           = flag.String("ca", "/root/ca.pem", "CA File location")
	MasterIP     = flag.String("master-ip", "", "IP Address for Master Components")
)
