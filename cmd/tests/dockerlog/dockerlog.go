package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"

	"golang.org/x/net/context"

	dockerstdcopy "github.com/docker/docker/pkg/stdcopy"
	dockerclient "github.com/docker/engine-api/client"
	dockertypes "github.com/docker/engine-api/types"
)

func main() {
	args := os.Args
	if len(args) != 2 {
		fmt.Printf("Usage:\n\tdockerlogs <container id>\n")
		return
	}

	id := args[1]

}
