package main

import (
	"fmt"
	"io/ioutil"
	"os"

	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"github.com/apporbit/infranetes/cmd/tests/bulk/api"
)

func main() {
	conn, err := grpc.Dial("127.0.0.1:4567", grpc.WithInsecure())
	if err != nil {
		fmt.Printf("failed to dial: %v\n", err)
		os.Exit(1)
	}
	client := api.NewBulkTestClient(conn)

	bytes, err := ioutil.ReadFile("/tmp/test")
	if err != nil {
		fmt.Printf("Failed to read file /tmp/test: %v\n", err)
		os.Exit(1)
	}

	fd, err := os.Open("/tmp/test")
	if err != nil {
		fmt.Printf("failed top open /tmp/test: %v\n", err)
		os.Exit(1)
	}

	fi, err := fd.Stat()
	if err != nil {
		fmt.Printf("Failed to stat file /tmp/test: %v\n", err)
		os.Exit(1)
	}

	file := &api.File{
		Data: bytes,
		Size: fi.Size(),
	}

	_, err = client.UploadFiles(context.Background(), file)
}
