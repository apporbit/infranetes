package common

import (
	"io/ioutil"
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/golang/glog"

	libcontainercgroups "github.com/opencontainers/runc/libcontainer/cgroups"
)

const (
	// default resources while the pod level qos of kubelet pod is not specified.
	defaultCPUNumber         = 1
	defaultMemoryinMegabytes = 64

	// More details about these: http://kubernetes.io/docs/user-guide/compute-resources/
	// cpuQuotaCgroupFile is the `cfs_quota_us` value set by kubelet pod qos
	cpuQuotaCgroupFile = "cpu.cfs_quota_us"
	// memoryCgroupFile is the `limit_in_bytes` value set by kubelet pod qos
	memoryCgroupFile = "memory.limit_in_bytes"
	// cpuPeriodCgroupFile is the `cfs_period_us` value set by kubelet pod qos
	cpuPeriodCgroupFile = "cpu.cfs_period_us"

	MiB = 1024 * 1024
)

func GetCpuLimitFromCgroup(cgroupParent string) (int32, error) {
	mntPath, err := libcontainercgroups.FindCgroupMountpoint("cpu")
	if err != nil {
		return -1, err
	}
	cgroupPath := filepath.Join(mntPath, cgroupParent)
	cpuQuota, err := readCgroupFileToInt64(cgroupPath, cpuQuotaCgroupFile)
	if err != nil {
		return -1, err
	}
	cpuPeriod, err := readCgroupFileToInt64(cgroupPath, cpuPeriodCgroupFile)
	if err != nil {
		return -1, err
	}

	// HyperContainer only support int32 vcpu number, and we need to use `math.Ceil` to make sure vcpu number is always enough.
	vcpuNumber := float64(cpuQuota) / float64(cpuPeriod)
	return int32(math.Ceil(vcpuNumber)), nil
}

// GetMemeoryLimitFromCgroup get the memory limit from given cgroupParent
func GetMemeoryLimitFromCgroup(cgroupParent string) (int32, error) {
	mntPath, err := libcontainercgroups.FindCgroupMountpoint("memory")
	if err != nil {
		return -1, err
	}
	cgroupPath := filepath.Join(mntPath, cgroupParent)
	memoryInBytes, err := readCgroupFileToInt64(cgroupPath, memoryCgroupFile)
	if err != nil {
		return -1, err
	}

	memoryinMegabytes := int32(memoryInBytes / MiB)
	// HyperContainer requires at least 64Mi memory
	if memoryinMegabytes < defaultMemoryinMegabytes {
		memoryinMegabytes = defaultMemoryinMegabytes
	}
	return memoryinMegabytes, nil
}

func readCgroupFileToInt64(cgroupPath, cgroupFile string) (int64, error) {
	contents, err := ioutil.ReadFile(filepath.Join(cgroupPath, cgroupFile))
	if err != nil {
		return -1, err
	}
	strValue := strings.TrimSpace(string(contents))
	if value, err := strconv.ParseInt(strValue, 10, 64); err == nil {
		return value, nil
	} else {
		return -1, err
	}
}

type annotationConfig struct {
	StartProxy     bool
	CreateInteface bool
	SetHostname    bool
}

func ParseCommonAnnotations(annotations map[string]string) *annotationConfig {
	ret := &annotationConfig{
		StartProxy:     true,
		CreateInteface: true,
		SetHostname:    true,
	}

	if a, ok := annotations["infranetes.startproxy"]; ok {
		b, err := strconv.ParseBool(a)
		if err != nil {
			glog.Infof("Couldn't parse bool %v for infranetes.startproxy: %v", a, err)
		} else {
			ret.StartProxy = b
		}
	}

	if a, ok := annotations["infranetes.createinterface"]; ok {
		b, err := strconv.ParseBool(a)
		if err != nil {
			glog.Infof("Couldn't parse bool %v for infranetes.createinterface: %v", a, err)
		} else {
			ret.CreateInteface = b
		}
	}

	if a, ok := annotations["infranetes.sethostname"]; ok {
		b, err := strconv.ParseBool(a)
		if err != nil {
			glog.Infof("Couldn't parse bool %v for infranetes.sethostname: %v", a, err)
		} else {
			ret.SetHostname = b
		}
	}

	return ret
}
