# Infranetes

Infranetes is a kubernetes container runtime that leverages virtual machine infrastructure instead of simply running pods as containers
 
It provides two ways of running pods

1) Run traditional pods isolated into independent virtual machines.
2) Treat virtual machine images as pods

By running individual pods within independent virtual machines, Infranetes provides better isolation and security for the containers run within the pod.
In a traditional Kubernetes setup, many pods are multiplexed together on a single node.
In general, Linux namespaces do a good job of isolating containers, however, they depend on the security of the underlying operating system.
As we've seen with exploits such as [Dirty COW](https://dirtycow.ninja/) this is not as strong a guarantee as we would like, as it results in a very large Trusted Computing Base (TCB).
By running pods within VM provided by a thin hypervisor, one can reduce the TCB tremendously which benefits pods run with Infranetes.

In addition, Infranetes allows one to orchestrate workloads that are not containerized today or might never be containerized.
For example, many windows applications cannot be containerized even with the new container support provided by Windows 10 and Windows Server 2016.
Only applications that can both be installed and run headless can be containizers.
In addition, one might have applications tied to older operating versions of Windows.
Infranetes lets one treat an entire VM image as a single container pod.
To Kubernetes, it will be able to make use of all Kubernetes features, including health checks, serving as service endpoints, making use of secrets and configmaps.... 

## Differences From Other Approaches

The first approach listed above is similar to Hyper and Rkt/KVM.
These provide more secure ways to run traditional pods by running them within hypervisor isolated virtual machines.
The primary difference is that in those cases the virtual machine has to live on the kubernetes node.
This works fine on bare metal and in private clouds that support nested virtualization.
However, in public clouds nested virtualization is not available and these mechanisms are not available.

## Architecture

Infranetes is composed of 2 main program components

1) The `infranetes` cri implementation that runs on a kubelet managed node.
`infranetes` communicates with `kubelet` over a unix domain socket with Kubelet's GRPC Container Runtime Interface (CRI).

2) The `vmserver` that runs within each provisioned virtual machine.
`vmserver` communicates with `infranetes` over a TLS protected TCP/IP socket using infranetes own GRPC protocol.

Infranetes works around the concept of pluggable PodProviders and ContainerProviders.

A PodProvider implements the APIs that manage the life cycle and status of the CRI's pod sandbox.

A ContainerProvider implements the APIs that manage the life cycle, status and images of containers that run within a pod sandbox  

## Current Status

Its currently pre-alpha.  Many things work, but the 3 most notable things that have not been implemented are

1) exporting of container logs via the API: ex so one can do `kubectl logs`

2) metrics related to pod usage. This would be important for Horizontal Pod Autoscalling (HPA)

3) Volume support.  Namely, one would expect to be able to use Kubernetes concept of persistent volumes within a public cloud.
For example to be able make use of EBS volumes with a cluster running on AWS.
However the current implementation of all the volumes providers is very node specific and expects them to always just live within the node.
We demonstrate NFS volumes working within pods managed by Infranetes, but this works as NFS can be mounted in many distinct locations at the same time.
This approach would not work for block devices.

[Installation](INSTALLATION.md)

