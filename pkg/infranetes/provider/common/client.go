/* RPC Client to connect and interact with vmserver */

package common

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"

	"github.com/sjpotter/infranetes/cmd/infranetes/flags"

	"github.com/docker/libnetwork/Godeps/_workspace/src/golang.org/x/net/context"
	"github.com/golang/glog"
	"github.com/sjpotter/infranetes/pkg/common"
	"time"
)

type Client struct {
	kubeclient kubeapi.RuntimeServiceClient
	vmclient   common.VMServerClient
	conn       *grpc.ClientConn
	lock       sync.Mutex
}

func (c *Client) CreateContainer(req *kubeapi.CreateContainerRequest) (*kubeapi.CreateContainerResponse, error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	resp, err := c.kubeclient.CreateContainer(context.Background(), req)

	return resp, err
}

func (c *Client) StartContainer(req *kubeapi.StartContainerRequest) (*kubeapi.StartContainerResponse, error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	resp, err := c.kubeclient.StartContainer(context.Background(), req)

	return resp, err
}

func (c *Client) StopContainer(req *kubeapi.StopContainerRequest) (*kubeapi.StopContainerResponse, error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	resp, err := c.kubeclient.StopContainer(context.Background(), req)

	return resp, err
}

func (c *Client) RemoveContainer(req *kubeapi.RemoveContainerRequest) (*kubeapi.RemoveContainerResponse, error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	resp, err := c.kubeclient.RemoveContainer(context.Background(), req)

	return resp, err
}

func (c *Client) ListContainers(req *kubeapi.ListContainersRequest) (*kubeapi.ListContainersResponse, error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	resp, err := c.kubeclient.ListContainers(context.Background(), req)

	return resp, err
}

func (c *Client) ContainerStatus(req *kubeapi.ContainerStatusRequest) (*kubeapi.ContainerStatusResponse, error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	resp, err := c.kubeclient.ContainerStatus(context.Background(), req)

	return resp, err
}

func (c *Client) StartProxy(ip string) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	_, err := c.vmclient.StartProxy(context.Background(), &common.IPAddress{Ip: ip})

	return err
}

func (c *Client) Close() {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.conn.Close()
}

func CreateClient(ip string) (*Client, error) {
	var (
		err    error
		client *Client
	)

	for i := 0; i < 10; i++ {
		client, err = internalCreateClient(ip)
		if err == nil {
			version, err1 := client.kubeclient.Version(context.Background(), &kubeapi.VersionRequest{})
			if err1 == nil {
				glog.Infof("CreateClient: version = %+v", version)
				err2 := client.StartProxy(ip)
				if err2 != nil {
					glog.Warningf("Couldn't start kube-proxy: %v", err2)
				}

				glog.Infof("Waiting on Docker")
				for j := 0; j < 5; j++ {
					_, err := client.ListContainers(&kubeapi.ListContainersRequest{})
					if err != nil {
						glog.Infof("CreateClient: docker isn't ready (%d): %v", j, err)
						time.Sleep(5 * time.Second)
					} else {
						glog.Infof("CreateClient: docker is ready")
						break
					}
				}
				return client, nil
			}
			glog.Infof("CreateClient: version failed: %v", err1)
			client.Close()
			err = err1
		} else {
			glog.Infof("CreateClient: internalCreateClient failed: %v", err)
		}
		time.Sleep(5 * time.Second)
	}

	return nil, err
}

func internalCreateClient(ip string) (*Client, error) {
	var opts []grpc.DialOption
	var creds credentials.TransportCredentials
	var sn = "127.0.0.1"

	creds, err := NewClientTLSFromFile(*flags.CA, sn)
	if err != nil {
		return nil, err
	}
	opts = append(opts, grpc.WithTransportCredentials(creds))

	conn, err := grpc.Dial(ip+":2375", opts...)

	if err != nil {
		return nil, err
	}

	kubeclient := kubeapi.NewRuntimeServiceClient(conn)
	vmclient := common.NewVMServerClient(conn)

	return &Client{kubeclient: kubeclient, vmclient: vmclient, conn: conn}, nil
}

// NewClientTLSFromFile constructs a TLS from the input certificate file for client.
func NewClientTLSFromFile(certFile, serverName string) (credentials.TransportCredentials, error) {
	b, err := ioutil.ReadFile(certFile)
	if err != nil {
		return nil, err
	}
	cp := x509.NewCertPool()
	if !cp.AppendCertsFromPEM(b) {
		return nil, fmt.Errorf("credentials: failed to append certificates")
	}
	return credentials.NewTLS(&tls.Config{ServerName: serverName, RootCAs: cp, InsecureSkipVerify: true}), nil
}
