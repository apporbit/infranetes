/* RPC Client to connect and interact with vmserver */

package common

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/golang/glog"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/sjpotter/infranetes/cmd/infranetes/flags"
	"github.com/sjpotter/infranetes/pkg/common"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
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

func (c *Client) StartProxy() error {
	c.lock.Lock()
	defer c.lock.Unlock()

	data, err := ioutil.ReadFile(*flags.Kubeconfig)

	req := &common.StartProxyRequest{
		ClusterCidr: *flags.ClusterCIDR,
		Ip:          *flags.MasterIP,
		Kubeconfig:  data,
	}

	_, err = c.vmclient.StartProxy(context.Background(), req)

	return err
}

func (c *Client) RunCmd(req *common.RunCmdRequest) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	_, err := c.vmclient.RunCmd(context.Background(), req)

	return err
}

func (c *Client) SetPodIP(ip string) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	_, err := c.vmclient.SetPodIP(context.Background(), &common.SetIPRequest{Ip: ip})

	return err
}

func (c *Client) GetPodIP() (string, error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	resp, err := c.vmclient.GetPodIP(context.Background(), &common.GetIPRequest{})
	if err != nil {
		return "", err
	}

	return resp.Ip, err
}

func (c *Client) SetSandboxConfig(config *kubeapi.PodSandboxConfig) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	bytes, err := json.Marshal(config)
	if err != nil {
		return err
	}

	_, err = c.vmclient.SetSandboxConfig(context.Background(), &common.SetSandboxConfigRequest{Config: bytes})

	return err
}

func (c *Client) GetSandboxConfig() (*kubeapi.PodSandboxConfig, error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	resp, err := c.vmclient.GetSandboxConfig(context.Background(), &common.GetSandboxConfigRequest{})
	if err != nil {
		return nil, err
	}

	var config kubeapi.PodSandboxConfig
	err = json.Unmarshal(resp.Config, &config)

	return &config, err
}

func (c *Client) CopyFile(file string) error {
	stat, err := os.Stat(file)
	if err != nil {
		return fmt.Errorf("Copyfile: Stat failed: %v", err)
	}
	if !stat.IsDir() {
		glog.Infof("CopyFile: copying %v", file)
		return c.internalCopyFile(file)
	}

	glog.Infof("CopyFile: %v is a directory, copying its contents", file)

	files, err := filepath.Glob(file + "/*")
	if err != nil {
		return fmt.Errorf("Copyfile: Glob failed: %v", err)
	}

	for _, f := range files {
		err := c.CopyFile(f)
		if err != nil {
			glog.Warningf("CopyFile: failed to copy %v: %v", f, err)
		}
	}

	return nil
}

func (c *Client) internalCopyFile(file string) error {
	fileData, err := ioutil.ReadFile(file)
	if err != nil {
		return fmt.Errorf("internalCopyFile: ReadFile failed: %v", err)
	}

	req := &common.CopyFileRequest{
		File:     file,
		FileData: fileData,
	}

	_, err = c.vmclient.CopyFile(context.Background(), req)

	return err
}

func (c *Client) MountFs(source string, target string, fstype string, readOnly bool) error {
	req := &common.MountFsRequest{
		Source:   source,
		Target:   target,
		Fstype:   fstype,
		ReadOnly: readOnly,
	}

	_, err := c.vmclient.MountFs(context.Background(), req)

	return err
}

func (c *Client) SetHostname(hostname string) error {
	req := &common.SetHostnameRequest{
		Hostname: hostname,
	}

	_, err := c.vmclient.SetHostname(context.Background(), req)

	return err
}

func (c *Client) Close() {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.conn.Close()
}

func CreateClient(ip string) (*Client, error) {
	glog.Infof("CreateClient: ip = %v", ip)
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
