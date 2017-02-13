package main

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"

	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"github.com/sjpotter/infranetes/cmd/tests/bulk/api"
)

type bulkServer struct{}

func (b *bulkServer) UploadFiles(ctx context.Context, file *api.File) (*api.UploadResponse, error) {
	err := ioutil.WriteFile("/tmp/dat1", file.Data, 0644)
	if err != nil {
		return nil, err
	}
	fd, err := os.Open("/tmp/dat1")
	if err != nil {
		return nil, err
	}
	fi, err := fd.Stat()
	if err != nil {
		return nil, err
	}
	if fi.Size() != file.Size {
		return nil, fmt.Errorf("File size (%d) not expected (%d)", fi.Size(), file.Size)
	}

	return &api.UploadResponse{}, nil
}

func main() {
	server := &bulkServer{}
	grpcServer := grpc.NewServer()

	lis, err := net.Listen("tcp", ":4567")
	if err != nil {
		fmt.Printf("couldn't bind to 4567: %v", err)
		os.Exit(1)
	}

	api.RegisterBulkTestServer(grpcServer, server)
	grpcServer.Serve(lis)
}
