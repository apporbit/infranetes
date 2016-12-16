package main

import (
	"fmt"

	"golang.org/x/net/context"

	dockerclient "github.com/docker/engine-api/client"
)

func main() {
	client, _ := dockerclient.NewClient(dockerclient.DefaultDockerHost, "", nil, nil)
	ver, _ := client.ServerVersion(context.Background())
	fmt.Printf("version = %+v\n", ver)
}
