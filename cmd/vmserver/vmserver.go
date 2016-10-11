package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/sjpotter/infranetes/pkg/vmserver"
)

const (
	infranetesVersion = "0.1"
)

var (
	version = flag.Bool("version", false, "Print version and exit")
	listen  = flag.Int("listen", 2375, "The listening port")
	cert    = flag.String("cert", "/root/cert.pem", "Location of certificate file")
	key     = flag.String("key", "/root/key.pem", "Location of key file")
)

func main() {
	flag.Parse()

	if *version {
		fmt.Printf("infranetes version: %s\n", infranetesVersion)
		os.Exit(0)
	}

	server, err := vmserver.NewVMServer(cert, key)
	if err != nil {
		fmt.Println("Initialize infranetes vm server failed: ", err)
		os.Exit(1)
	}

	fmt.Println(server.Serve(*listen))
}
