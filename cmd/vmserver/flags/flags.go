package flags

import "flag"

var (
	Version      = flag.Bool("version", false, "Print version and exit")
	Listen       = flag.Int("listen", 2375, "The listening port")
	Cert         = flag.String("cert", "/root/cert.pem", "Location of certificate file")
	Key          = flag.String("key", "/root/key.pem", "Location of key file")
	ContProvider = flag.String("contprovider", "docker", "Container Provider to use")
)
