# Infranetes

Infranetes overview here

## Installation

Installing Infranetes is currently a relatively manual affair.  

For these instructions we assume you can already create a kubernetes cluster (and these instructions assume it was via `cluster/kube-up.sh` on aws from kubernetes)

1. Build infranetes and vmserver

2. Create CA certs and keys for use by the GRPC communication between `infranetes` and `vmserver` 

3. Create a base linux image to be used as pod host that starts Docker and `vmserver` on boot
 
4. Changing kubelet on an existing node to use infranetes as its container runtime via the CRI

5. Labeling and taininting the node to ensure that only pods meant to be scheduled via infranetes are scheduled to this node

### 1. Building infranetes and vmserver

This is fairly straight forward go build

```bash
$ go build ./cmd/ifranetes/infranetes.go
$ go build ./cmd/vmserver/vmserver.go
```

### 2. Creating CA Certs and keys

1. Create a CA public/private key pair

```bash
$ openssl genrsa -aes256 -out ca.pem 4096
```

2. Create a server key pair 
 
```bash
$ openssl genrsa -out key.pem 4096
```

3. Create a certificate signing request for it
```bash
$ openssl req -subj "/CN=127.0.0.1" -sha256 -new -key key.pem -out server.csr
```

4. Sign it with the CA key created above

```bash
$ echo subjectAltName = DNS:127.0.0.1,IP:127.0.0.1 > extfile.cnf

$ openssl x509 -req -days 365 -sha256 -in server.csr -CA ca.pem -CAkey ca.pem -CAcreateserial -out cert.pem -extfile extfile.cnf
```

this will result in 3 files we care about for infranetes usage `ca.pem`, `key.pem`, and `cert.pem`

In the current version of Infranetes we use the InsecureSkipVerify setting of TLS and simply rely on the fact that the key was signed by the CA key we have access to

### 3. Creating the base image

In amazon, the way we currently create the base image  

1. boot a regular ubuntu instance provided by AWS

2. scp `vmserver`, init files and server key/cert to the new instance

`$ scp -i <ec2-key location> vmserver key.pem cert.pem vmserver.init ubuntu@<ec2 instance ip>:/tmp`

3. ssh to the instance and move files to the appropriate location

```bash
$ ssh -i <ec2-key location> ubuntu@<ec2_instance_ip>

<connect to vm>

$ sudo -s
# mv /tmp/vmserver /usr/local/sbin
# mv /tmp/*.pem /root
# mv /tmp/vmserver.init /etc/init.d/vmserver
# systemctl daemon-reload
```

4. use aws to image this VM and one can name this image infranetes-base.  This AMI will be the image infrantes boot to act as a pod host

### 4. Modify an existing node to act as an infranetes node.

This section currently assumes that one created an aws kubernetes cluster with `cluster/kube-up.sh` from the kubernetes source repository
 
1. create a `vars.sh` file that will contain one's AWS keys

```bash
export AWS_ACCESS_KEY_ID=<fill in>
export AWS_SECRET_ACCESS_KEY=<fil in>
```

2. create an `aws.json` that corresponds to one's kubernetes cluster configuration

```json
{
 "Ami":"<ami-id created above>",
 "RouteTable":"<rtb-id created by kube-up>",
 "Region":"<region running in",
 "SecurityGroup":"<sg-id created by kube-up>",
 "Vpc":"<vpc-id created by kube-up>",
 "Subnet":"<subnet-id created by kube-up>",
 "SshKey":"<key used to connect to ubuntu account>"
}
```

3. copy `infranetes`, `ca.pem`, `vars.sh` and `aws.json` to the node being modified and move to `/root`

4. modify `kubelet` via `/etc/sysconfig/kubelet` to use `infranetes` via the cri
  
```bash
$ vi /etc/sysconfig/kubelet

change DAEMON_ARGS to

DAEMON_ARGS="$DAEMON_ARGS --api-servers=https://172.20.0.9 --enable-debugging-handlers=true  --hostname-override=ip-172-20-0-57.us-west-2.compute.internal --cloud-provider=aws  --config=/etc/kubernetes/manifests  --allow-privileged=True --v=4 --cluster-dns=10.0.0.10 --cluster-domain=cluster.local    --non-masquerade-cidr=10.0.0.0/8  --babysit-daemons=true  --container-runtime=remote --container-runtime-endpoint=/tmp/infra "
```

5. on the node, run infranetes as root (I currently use a screen/tmux session for this)

```bash
# ./infranetes -alsologtostderr -listen /tmp/infra -podprovider aws -master-ip 172.20.0.9 -base-ip 10.244.10
```

6. on the node restart kubelet to use the new configuration

```bash
# systemctl restart kubelet
```

### 5. Label/Taint the new Infranetes node

on a macine that can use kubectl to manage the kubernetes cluster label and taint this node

```bash
$ kubectl taint node <name> infranetes=true:NoSchedule
$ kubectl label node <name> infrantes=true
```
---
Congratulations, you should know have a working kubernetes cluster that can selected pods into independent VMs

