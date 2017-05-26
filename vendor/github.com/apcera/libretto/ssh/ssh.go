// Copyright 2015 Apcera Inc. All rights reserved.

package ssh

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	cssh "golang.org/x/crypto/ssh"
)

var (
	// ErrInvalidUsername is returned when the username is invalid.
	ErrInvalidUsername = errors.New("A valid username must be supplied")
	// ErrInvalidAuth is returned when the username is invalid.
	ErrInvalidAuth = errors.New("Invalid authorization method: missing password or key")
	// ErrSSHInvalidMessageLength is returned when the scp implementation gets an invalid number of messages.
	ErrSSHInvalidMessageLength = errors.New("Invalid message length")
	// ErrTimeout is returned when a timeout occurs waiting for sshd to respond.
	ErrTimeout = errors.New("Timed out waiting for sshd to respond")
	// ErrKeyGeneration is returned when the library fails to generate a key.
	ErrKeyGeneration = errors.New("Unable to generate key")
	// ErrValidation is returned when we fail to validate a key.
	ErrValidation = errors.New("Unable to validate key")
	// ErrPublicKey is returned when gossh fails to parse the public key.
	ErrPublicKey = errors.New("Unable to convert public key")
	// ErrUnableToWriteFile is returned when the library fails to write to a file.
	ErrUnableToWriteFile = errors.New("Unable to write file")
	// ErrNotImplemented is returned when a function is not implemented (typically by the Mock implementation).
	ErrNotImplemented = errors.New("Operation not implemented")
)

const (
	sshPort = 22

	// PasswordAuth represents password based auth.
	PasswordAuth = "password"

	// KeyAuth represents key based authentication.
	KeyAuth = "key"

	// Timeout for connecting to an SSH server.
	Timeout = 60 * time.Second
)

// Client represents an interface for abstracting common ssh operations.
type Client interface {
	Connect() error
	Disconnect()
	Download(src io.WriteCloser, dst string) error
	Run(command string, stdout io.Writer, stderr io.Writer) error
	Upload(src io.Reader, dst string, mode uint32) error
	Validate() error
	WaitForSSH(maxWait time.Duration) error

	SetSSHPrivateKey(string)
	GetSSHPrivateKey() string
	SetSSHPassword(string)
	GetSSHPassword() string
}

// Credentials supplies SSH credentials.
type Credentials struct {
	mu            sync.Mutex
	SSHUser       string
	SSHPassword   string
	SSHPrivateKey string
}

// Options provides SSH options like KeepAlive.
type Options struct {
	IPs       []net.IP
	KeepAlive int
	Pty       bool
}

// SSHClient provides details for the SSH connection.
type SSHClient struct {
	Creds   *Credentials
	IP      net.IP
	Port    int
	Options Options

	cryptoClient *cssh.Client
	close        chan bool
}

// Connect connects to a machine using SSH.
func (client *SSHClient) Connect() error {
	var (
		auth cssh.AuthMethod
		err  error
	)

	if err = client.Validate(); err != nil {
		return err
	}

	if client.Creds.SSHPrivateKey != "" {
		auth, err = getAuth(client.Creds, KeyAuth)
		if err != nil {
			return err
		}
	} else {
		auth, err = getAuth(client.Creds, PasswordAuth)
		if err != nil {
			return err
		}
	}

	config := &cssh.ClientConfig{
		User: client.Creds.SSHUser,
		Auth: []cssh.AuthMethod{
			auth,
		},
		HostKeyCallback: cssh.InsecureIgnoreHostKey(),
	}

	port := sshPort
	if client.Port != 0 {
		port = client.Port
	}

	c, err := dial("tcp", fmt.Sprintf("%s:%d", client.IP, port), config)
	if err != nil {
		return err
	}

	client.cryptoClient = c

	client.close = make(chan bool, 1)

	if client.Options.KeepAlive > 0 {
		go client.keepAlive()
	}
	return nil
}

func (client *SSHClient) keepAlive() {
	t := time.NewTicker(time.Duration(client.Options.KeepAlive) * time.Second)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			// send a keep alive request on the underlying channel
			client.cryptoClient.Conn.SendRequest("libretto-ssh", true, nil)
		case <-client.close:
			// client is disconnecting, close it
			return
		}
	}
}

// Disconnect should be called when the ssh client is no longer needed, and state can be cleaned up
func (client *SSHClient) Disconnect() {
	client.close <- true
}

// Download downloads a file via SSH (SCP)
func (client *SSHClient) Download(dst io.WriteCloser, remotePath string) error {
	defer dst.Close()

	session, err := client.cryptoClient.NewSession()
	if err != nil {
		return err
	}

	defer session.Close()

	ackPipe, err := session.StdinPipe()
	if err != nil {
		return err
	}

	dataPipe, err := session.StdoutPipe()
	if err != nil {
		return err
	}

	errorChan := make(chan error, 3)

	wg := sync.WaitGroup{}
	wg.Add(3)

	// This goroutine is for writing the scp header message.
	go func() {
		defer wg.Done()

		defer ackPipe.Close()

		// 3 ack messages; 1 to initiate, 1 for the message, 1 for the data
		// https://blogs.oracle.com/janp/entry/how_the_scp_protocol_works
		fmt.Fprintf(ackPipe, string(byte(0)))
		fmt.Fprintf(ackPipe, string(byte(0)))
		fmt.Fprintf(ackPipe, string(byte(0)))
	}()

	// This goroutine is for downloading the file.
	go func() {
		defer wg.Done()

		// First line of data is permissions, size, and name.
		// For example: C0666 14 somefile
		// Use the permissions for the file, discard size and name
		// https://blogs.oracle.com/janp/entry/how_the_scp_protocol_works
		dr := bufio.NewReader(dataPipe)
		s, err := dr.ReadString('\n')
		if err != nil {
			errorChan <- err
			return
		}
		scpMsgs := strings.Split(s, " ")

		// Only currently support copying single files
		if len(scpMsgs) != 3 || len(scpMsgs[0]) != 5 || scpMsgs[0][0] != 'C' {
			errorChan <- ErrSSHInvalidMessageLength
			return
		}

		// Get the length of the expected data; scp control messages follow.
		length, err := strconv.ParseInt(scpMsgs[1], 10, 64)
		if err != nil {
			errorChan <- err
			return
		}

		// Copy content to file
		_, err = io.CopyN(dst, dr, length)
		if err != nil {
			errorChan <- err
			return
		}
	}()

	go func() {
		defer wg.Done()

		// On the remote machine run scp in source mode to send the files.
		err := session.Run(fmt.Sprintf("/usr/bin/scp -f %s", remotePath))
		if err != nil {
			errorChan <- err
		}
	}()

	wg.Wait()

	select {
	case err := <-errorChan:
		return err
	default:
		return nil
	}
}

// Run runs a command via SSH.
func (client *SSHClient) Run(command string, stdout io.Writer, stderr io.Writer) error {
	session, err := client.cryptoClient.NewSession()
	if err != nil {
		return err
	}

	defer session.Close()

	session.Stdout = stdout
	session.Stderr = stderr

	if client.Options.Pty {
		modes := cssh.TerminalModes{
			cssh.ECHO:          0,
			cssh.TTY_OP_ISPEED: 14400,
			cssh.TTY_OP_OSPEED: 14400,
		}
		// Request pseudo terminal
		if err := session.RequestPty(os.Getenv("TERM"), 80, 40, modes); err != nil {
			return err
		}
	}

	return session.Run(command)
}

// Upload uploads a new file via SSH (SCP)
func (client *SSHClient) Upload(src io.Reader, dst string, mode uint32) error {
	fileContent, err := ioutil.ReadAll(src)
	if err != nil {
		return err
	}

	session, err := client.cryptoClient.NewSession()
	if err != nil {
		return err
	}

	defer session.Close()

	w, err := session.StdinPipe()
	if err != nil {
		return err
	}

	errorChan := make(chan error, 2)
	remoteDir := path.Dir(dst)
	remoteFileName := path.Base(dst)

	wg := sync.WaitGroup{}
	wg.Add(2)

	go func() {
		defer w.Close()
		defer wg.Done()

		// Signals to the SSH receiver that content is being passed.
		fmt.Fprintf(w, "C%#o %d %s\n", mode, len(fileContent), remoteFileName)
		_, err = io.Copy(w, bytes.NewReader(fileContent))
		if err != nil {
			errorChan <- err
			return
		}

		// End SSH transfer
		fmt.Fprint(w, "\x00")
	}()

	go func() {
		defer wg.Done()
		if err := session.Run(fmt.Sprintf("/usr/bin/scp -t %s", remoteDir)); err != nil {
			errorChan <- err
			return
		}
	}()

	wg.Wait()

	select {
	case err := <-errorChan:
		return err
	default:
		break
	}

	return nil
}

// Validate verifies that SSH connection credentials were properly configured.
func (client *SSHClient) Validate() error {
	if client.Creds.SSHUser == "" {
		return ErrInvalidUsername
	}

	if client.Creds.SSHPrivateKey == "" && client.Creds.SSHPassword == "" {
		return ErrInvalidAuth
	}

	return nil
}

// WaitForSSH will try to connect to an SSH server. If it fails, then it'll
// sleep for 5 seconds.
func (client *SSHClient) WaitForSSH(maxWait time.Duration) error {
	start := time.Now()

	for {
		if err := client.Connect(); err == nil {
			defer client.Disconnect()
			return nil
		}

		timePassed := time.Since(start)
		if timePassed >= maxWait {
			break
		}

		time.Sleep(5 * time.Second)
	}

	return ErrTimeout
}

// SetSSHPrivateKey sets the private key on the clients credentials.
func (client *SSHClient) SetSSHPrivateKey(s string) {
	client.Creds.mu.Lock()
	client.Creds.SSHPrivateKey = s
	client.Creds.mu.Unlock()
}

// GetSSHPrivateKey gets the private key on the clients credentials.
func (client *SSHClient) GetSSHPrivateKey() string {
	client.Creds.mu.Lock()
	defer client.Creds.mu.Unlock()
	return client.Creds.SSHPrivateKey
}

// SetSSHPassword sets the SSH password on the clients credentials.
func (client *SSHClient) SetSSHPassword(s string) {
	client.Creds.mu.Lock()
	client.Creds.SSHPassword = s
	client.Creds.mu.Unlock()
}

// GetSSHPassword gets the SSH password on the clients credentials.
func (client *SSHClient) GetSSHPassword() string {
	client.Creds.mu.Lock()
	defer client.Creds.mu.Unlock()
	return client.Creds.SSHPassword
}

// dial will attempt to connect to an SSH server.
var dial = func(network, addr string, config *cssh.ClientConfig) (*cssh.Client, error) {
	d := net.Dialer{Timeout: Timeout, KeepAlive: 2 * time.Second}

	conn, err := d.Dial(network, addr)
	if err != nil {
		return nil, err
	}

	c, chans, reqs, err := cssh.NewClientConn(conn, addr, config)
	if err != nil {
		return nil, err
	}

	return cssh.NewClient(c, chans, reqs), nil
}

var readPrivateKey = func(key string) (cssh.AuthMethod, error) {
	signer, err := cssh.ParsePrivateKey([]byte(key))
	if err != nil {
		return nil, err
	}

	return cssh.PublicKeys(signer), nil
}

var getAuth = func(c *Credentials, authType string) (cssh.AuthMethod, error) {
	var (
		auth cssh.AuthMethod
		err  error
	)

	switch authType {
	case PasswordAuth:
		return cssh.Password(c.SSHPassword), nil
	case KeyAuth:
		return readPrivateKey(c.SSHPrivateKey)
	}
	return auth, err
}
