# Infranetes

Infranetes is a kubernetes container runtime interface (CRI) implemetation that enables users to manage IAAS virtual machines (VMs) in an analagous manner to how Kubernetes manages its Pod containers.
Instead of creating a pod that kubernetes will schedule to a single node in a pre-defined cluster, Infranetes allows users to create a VM instance that will be run by the underlying IAAS infrastucture.
This enables Kubernetes to manage a hybrid cluster of "modern" container based application deployments and "legacy" virtual machine image deployments. 

To Kubernetes, this VM will look and behave like a regular pod: 
It's cluster IP will be the Pod IP that Kubernetes sees.
Its processes will be able to access services provided by other pods within kubernetes cluster.
It will be able to be endpoints for services that processes in other pods will be able to access.
Kubernetes style management will apply to these  virtual machines.
For example, secrets and configmaps will be populated within the virtual machine.

In addition, Kubernetes will be able to execute health checks and resource monitor the VMs in the exact same way it manges traditional pods.
This means, that Kubernetes can start/restart/kill the VMs as needed based on horizontal scaling needs of the provided service and the health of the components making up the service.
Infranetes accomplishes this by using the same exact pod configuration mechanisms that are used today.

Infranetes' VMs are used within a Kubernetes cluster in 2 ways.

1) It can run "modern" container based applications, i.e. a traditional pod, isolated into independent virtual machines.
2) It can run a "legacy" virtual machine image based application that "looks and acts" like a traditional pod to the rest of the Kubernetes cluster

By running individual pods within independent virtual machines, Infranetes provides better isolation and security for the containers run within the pod.
In a traditional Kubernetes setup, many pods are multiplexed together on a single node.
In general, Linux namespaces do a good job of isolating containers, however, they depend on the security of the underlying operating system.
As we've seen with exploits such as [Dirty COW](https://dirtycow.ninja/) this is not as strong a guarantee as we would like, as it results in a very large Trusted Computing Base (TCB).
By running pods within VM provided by a thin hypervisor, one can reduce the TCB tremendously which benefits pods run with Infranetes.
More importantly, Infranetes allows one to leverge an IAAS service, such as AWS, Azure, or GCP, one can rely on the strong isolation guarantees provided by the service provider.
Instead of having to dedicate resource to ensure that one's private cloud installation remains secure and bug-free, a difficult task in general, one can delegate this responsibility to the large IAAS providers.
They dedicate significantly more resources to ensuring that the isolation provided to the virtual machine instances run on their platform than any single organization can dedicate to their own private cloud.
Additionally, container mechanisms don't provide the same level of performance as VMs. [[SysDig]](https://sysdig.com/blog/container-isolation-gone-wrong/).
While running individual pods within independent containers can result in a less efficient usage of resources, it providers stronger performance guarantees. 

In addition, Infranetes enables Kubernetes to treat a VM image booted to an VM instance as a single pod.
This allows these VM instances to leverage all of  Kubernetes features as described above. 
This enables Kubernetes to orchestrate workloads that are not containerized today or might never be containerized.
For example, many windows applications cannot be containerized even with the new container support provided by Windows 10 and Windows Server 2016.
For instance, only applications that can both be installed and run headless can be containizers.
In addition, one might have applications tied to older operating versions of Windows.
Even within Linux, which imposes very little restrictions on what can be packaged up as a container image, Infranetes gives you abilities that are not availale in Kubernetes today, such as to get access to raw block devices. 

## Differences From Other Approaches

The first approach listed above is similar to Hyper, Rkt/KVM, and Virtlet.
Hyper and Rkt/KVM provide more secure ways to run traditional pods by running them within hypervisor isolated virtual machines, while Virlet enables one to boot virtual machine images and have them look like Pods.
The primary difference is that in those cases the virtual machine has to live on the kubernetes node and one cannot leverage an IAAS provided layer.
In addition to the benefits of relying on an IAAS provider, these mechanism only work on bare metal and in private clouds that support nested virtualization.
If one is already running their Kubernetes cluster in an IAAS/public clouds environment that doesn't support nested virtualization, i.e. most, these mechanisms will also not be supported.

## Architecture

Infranetes is composed of 2 main program components

1) The `infranetes` cri implementation that runs on a kubelet managed node.
`infranetes` communicates with `kubelet` over a unix domain socket with Kubelet's GRPC Container Runtime Interface (CRI).

2) The `vmserver` agent that runs within each provisioned virtual machine.
`vmserver` communicates with `infranetes` over a TLS protected TCP/IP socket using infranetes own GRPC protocol.

Infranetes works around the concept of pluggable PodProviders and ContainerProviders.

A PodProvider implements the APIs that manage the life cycle and status of the CRI's pod sandbox.
Each PodProvider implements the IAAS's APIs for managing the VM intance lifecycle, including creating, booting, stopping, and removing the VM instances, as well as management of the IP address resources within the cloud.
The `infranetes` CRI implementation loads the specified PodProvider at runtime, enabling it to manage VM life cycle correctly.

A ContainerProvider implements the APIs that manage the life cycle, status and images of containers that run within a pod sandbox.  
In practice, these generally map directly to the CRIs API calls.  API calls for containers such as Create/Start/Remove/Status are just proxies to appropriate VM.
The `vmserver` agent loads the specified ContainerProvider at runtime, enabling infranetes ot manage a pod's container lifecycles correctly.
Listing containers, as the call is not pod specific, requires iterating over every VM and combining the results.
`vmserver` also implements `kube-proxy` enabling processes running within the VM to make use of kubernetes provided services by using network address translation to connect to the appropriate pod IP addresses.

`vmserver` implements a number of ContainerProviders.
These include:
- Docker: Enables Infranetes to run traditional pods and their containers within the VM providing the pod abstraction.
- Fake: Enables Infranetes to run VM images and make them appear to Kubernetes as a full featured pod.
- SystemD: A prototype CotainerProvider that enables one to push binaries to a base VM instance and control their life cycles via systemd.

In addition, Infrantes probably a Kubernetes `flexdriver` file system volume driver that can attach IAAS provided image volumes to the VM instances.
This allows cloud volumes to be associated with each Infranetes managed VM instance in the same manner that Kubernetes can attach cloud volumes to a traditional Pod instance. 

## Current Status

Its currently pre-alpha.  However, many things work.

1) Kubernetes runtime integration

- PodProviders have been built and tested for AWS, GCP, and Vsphere

- ContainerProviders have been built and tested for docker images and plain vm image
  - Docker ContainerProvider supports exporting logs through the CRI.  Other container providers need to be improved.
  Therefore, for traditional pods run within independent VMs,`kubectl logs` works as expected. 

2) Metric Support

- Kubernetes currently does't implement metric support into the CRI, it still depends on cadvisor providing the metrics on the node.  There is a proposal that will be implemented.  We've created a small patch to kubernetes to demonstrate scaling working in a slightly different way, but expect to convert to the standardized way once the kubelet has support for it.
   - This is important to support things such as Horizontal Pod Autoscalling (HPA)

3) Volume Support 
- Provide virtual disk volume attachment support for AWS and GCP.
  - These volums can be used as raw devices or regular file systems
- Propegate NFS kubernetes volumes to be mounted within the VM as well
- Propegate secret and configmap volume data to the VM


