# Installation

Installing Infranetes is currently a relatively manual affair.  

0. Bring up a cluster

1. Build infranetes and vmserver

2. Create CA certs and keys for use by the GRPC communication between `infranetes` and `vmserver` 

3. Create a base linux image to be used as pod host that starts Docker and `vmserver` on boot
 
4. Labeling and taininting the node to ensure that only pods meant to be scheduled via infranetes are scheduled to this node

5. Modify AWS VPC to work on the Intenet

## 0. Bring up a cluster

For these instructions, I'm basing it off a cluster brought up with [Kops](https://kubernetes.io/docs/getting-started-guides/kops/).
At the time of this writing, only an alpha version with Kubernetes 1.7 Support has been [released](https://github.com/kubernetes/kops/releases/download/1.7.0-alpha.1/kops-linux-amd64)

## 1. Building infranetes and vmserver

 This is fairly straight forward go build
    
 ```bash
 $ go build ./cmd/ifranetes/infranetes.go
 $ go build ./cmd/vmserver/vmserver.go
 ```

## 2. Creating CA Certs and keys

1. Create a CA public/private key pair

 ```bash
 $ openssl genrsa -aes256 -out ca-key.pem 4096
 $ openssl req -new -x509 -days 365 -key ca-key.pem -sha256 -out ca.pem
 ```

2. Create a server key pair 
 
 ```bash
 $ openssl genrsa -out key.pem 4096
 ```

3. Create a certificate signing request for it

 ```bash
 $ openssl req -subj "/CN=*" -sha256 -new -key key.pem -out server.csr
 ```

4. Sign it with the CA key created above

 ```bash
 $ echo subjectAltName = IP:127.0.0.1 > extfile.cnf

 $ openssl x509 -req -days 365 -sha256 -in server.csr -CA ca.pem -CAkey ca-key.pem -CAcreateserial -out cert.pem -extfile extfile.cnf
 ```
 this will result in 3 files we care about for infranetes usage `ca.pem`, `key.pem`, and `cert.pem`

 * Note the above IP in the subjectAltName isn't the IP of the VM instance, however, all that TLS cares is that `infranetes` thinks it should be 127.0.0.1 and vmserver claims it is 127.0.0.1 and the certificate chain verifies to the CA 

## 3. Creating the base image

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
 # chmod +x /etc/init.d/vmserver
 # ln -s /etc/init.d/vmserver /etc/rc2.d/S02vmserver
 # systemctl daemon-reload
 ```
 
4. Install conntrack (needed for kube-proxy)

```bash
# apt-get install conntrack
```

5. Install Docker.  I've followed the instructions [here](https://docs.docker.com/engine/installation/linux/docker-ce/ubuntu/).

6. use aws to image this VM and one can name this image infranetes-base.  This AMI will be the image infrantes boot to act as a pod host

## 4. Modify an existing Kubernetes node to act as an infranetes node.

Find a fairly empty node (i.e. only running kube-proxy) and remove kube-proxy from manifest dir.  I created a 4 node cluster ot help ensure this.
To determine if a node is empty, one can list the docker containers on using `docker ps -a` on each node.  Kops runs a `protokube` container on each node, but this can be ignored.
 
1. create a `vars.sh` file that will contain one's AWS keys

 ```bash
 export AWS_ACCESS_KEY_ID=<fill in>
 export AWS_SECRET_ACCESS_KEY=<fil in>
 ```
 
2. a) Creates a new subnet within the kubernetes vpc for use by infrantes.

   b) Add it to the kubernetes route table
    
   c) Configure it to auto assign a public ip address - *** This is very Important *** 

3. create an `aws.json` that corresponds to one's kubernetes cluster configuration

 ```json
 {
  "Ami":"<ami-id created above>",
  "RouteTable":"<rtb-id created by kube-up>",
  "Region":"<region running in",
  "SecurityGroup":"<sg-id created by kube-up>",
  "Vpc":"<vpc-id created by kube-up>",
  "Subnet":"<subnet-id created above>",
  "SshKey":"<key used to connect to ubuntu account>"
 }
 ```

4. copy `infranetes`, `ca.pem`, `vars.sh` and `aws.json` to the node being modified and move to `/root`

5. modify `kubelet` via `/etc/sysconfig/kubelet` to use `infranetes` via the cri
  
 ```bash
 $ vi /etc/sysconfig/kubelet

 change DAEMON_ARGS to
 
 DAEMON_ARGS="--allow-privileged=true --cgroup-root=/ --cloud-provider=aws --cluster-dns=100.64.0.10 --cluster-domain=cluster.local --enable-debugging-handlers=true --eviction-hard=memory.available<100Mi,nodefs.available<10%,nodefs.inodesFree<5%,imagefs.available<10%,imagefs.inodesFree<5% --hostname-override=ip-172-20-60-171.ec2.internal --kubeconfig=/var/lib/kubelet/kubeconfig --network-plugin=kubenet --node-labels=kubernetes.io/role=node,node-role.kubernetes.io/node= --non-masquerade-cidr=100.64.0.0/10 --pod-manifest-path=/etc/kubernetes/manifests --register-schedulable=true --require-kubeconfig=true --v=2 --container-runtime=remote --container-runtime-endpoint=/tmp/infra.sock"
 ```

6. on the node, run infranetes as root (I currently use a screen/tmux session for this), base -ip is based on the first 3 octets of the subnet one created in 4.2 above. 

```bash
 # ./infranetes -alsologtostderr -base-ip 172.20.10 -cluster-cidr 100.96.0.0/11 -listen /tmp/infra.sock -podprovider aws -master-ip api.internal.useast1.k8s.yucs.org
```
 
7. on the node restart kubelet to use the new configuration

 ```bash
 # systemctl restart kubelet
 ```

## 5. Label/Taint the new Infranetes node

on a macine that can use kubectl to manage the kubernetes cluster label and taint this node

 ```bash
 $ kubectl taint node <name> infranetes=true:NoSchedule
 $ kubectl label node <name> infrantes=true
 ```

Congratulations, you should know have a working kubernetes cluster that can selected pods into independent VMs

---

## Using it to run AMIs

much like one build the `infranetes-base` image above, one can modify any image to work with infranetes.

1. One can take any Linux AMI and copy `vmserver` to it as above, but `vmserver.init` will be modified to run have an added options of `-contprovider fake`

2. images that one creates (with `vmserver` added) needed to be tagged with 2 tags.  `infranetes=true` and `infranetes.image_name=<name>:<version>` ex: `nginix:latest`

3. `infranetes` will be run with a an added option of `-imgprovider aws`, this will instruct it to search for images in aws ami catalog
 
example: 
    
1. boot `infranetes-base`
 
2. ssh into it
    
3. `sudo apt-get install nginx`

4. modify `/etc/init.d/vmserver` to use DAEMON_OPTS with `-contprovider fake`
     
4. save as a new image "infranetes-nginx"
    
5. tag image as described above

6. restart `infranetes` with `-imgprovider aws`

See [demo/ami-image](demo/ami-image) for how one would use this ami image.
