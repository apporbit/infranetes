package main

import (
	"fmt"
	dockerclient "github.com/docker/engine-api/client"
	"github.com/docker/libnetwork/Godeps/_workspace/src/golang.org/x/net/context"
)

func main() {
	client, _ := dockerclient.NewClient(dockerclient.DefaultDockerHost, "", nil, nil)
	ver, _ := client.ServerVersion(context.Background())
	fmt.Printf("version = %+v\n", ver)
}
