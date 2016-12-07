/* Storing all global flags here for now to avoid circular imports */

package flags

import (
	"flag"
)

var (
	Version     = flag.Bool("version", false, "Print version and exit")
	Listen      = flag.String("listen", "/var/run/infra.sock", "The listen socket, e.g. /var/run/infra.sock")
	ConfigFile  = flag.String("config", "", "Configuration file")
	PodProvider = flag.String("podprovider", "virtualbox", "Pod Provider to use")
	ImgProvider = flag.String("imgprovider", "docker", "Container Image Provider to use")
	CA          = flag.String("ca", "/root/ca.pem", "CA File location")
	MasterIP    = flag.String("master-ip", "", "IP Address for Master Components")
	ClusterCIDR = flag.String("cluster-cidr", "", "The CIDR range of pods in the cluster. It is used to bridge traffic coming from outside of the cluster. If not provided, no off-cluster bridging will be performed.")
	Kubeconfig  = flag.String("kubeconfig", "/var/lib/kube-proxy/kubeconfig", "Path to kubeconfig file with authorization information (the master location is set by the master flag")
	IPBase      = flag.String("base-ip", "", "First 3 octets of the IP address")
)
