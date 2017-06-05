package vmserver

import (
	"fmt"

	"github.com/golang/glog"

	utildbus "k8s.io/kubernetes/pkg/util/dbus"
	"k8s.io/kubernetes/pkg/util/exec"
	utiliptables "k8s.io/kubernetes/pkg/util/iptables"
)

const (
	// the mark-for-masquerade chain
	// TODO: clean up this logic in kube-proxy
	KubeMarkMasqChain utiliptables.Chain = "KUBE-MARK-MASQ"

	// the mark-for-drop chain
	KubeMarkDropChain utiliptables.Chain = "KUBE-MARK-DROP"

	// kubernetes postrouting rules
	KubePostroutingChain utiliptables.Chain = "KUBE-POSTROUTING"

	// kubernetes firewall rules
	KubeFirewallChain utiliptables.Chain = "KUBE-FIREWALL"
)

func createTables() {
	protocol := utiliptables.ProtocolIpv4

	execer := exec.New()
	dbus := utildbus.New()
	iptClient := utiliptables.New(execer, dbus, protocol)

	if MasqueradeBit < 0 || MasqueradeBit > 31 {
		glog.Errorf("invalid iptables-masquerade-bit %v not in [0, 31]", MasqueradeBit)
		return
	}

	if DropBit < 0 || DropBit > 31 {
		glog.Errorf("invalid iptables-drop-bit %v not in [0, 31]", DropBit)
		return
	}

	if DropBit == MasqueradeBit {
		glog.Errorf("iptables-masquerade-bit %v and iptables-drop-bit %v must be different", MasqueradeBit, DropBit)
		return
	}

	// Setup KUBE-MARK-DROP rules
	dropMark := getIPTablesMark(DropBit)
	if _, err := iptClient.EnsureChain(utiliptables.TableNAT, KubeMarkDropChain); err != nil {
		glog.Errorf("Failed to ensure that %s chain %s exists: %v", utiliptables.TableNAT, KubeMarkDropChain, err)
		return
	}
	if _, err := iptClient.EnsureRule(utiliptables.Append, utiliptables.TableNAT, KubeMarkDropChain, "-j", "MARK", "--set-xmark", dropMark); err != nil {
		glog.Errorf("Failed to ensure marking rule for %v: %v", KubeMarkDropChain, err)
		return
	}
	if _, err := iptClient.EnsureChain(utiliptables.TableFilter, KubeFirewallChain); err != nil {
		glog.Errorf("Failed to ensure that %s chain %s exists: %v", utiliptables.TableFilter, KubeFirewallChain, err)
		return
	}
	if _, err := iptClient.EnsureRule(utiliptables.Append, utiliptables.TableFilter, KubeFirewallChain,
		"-m", "comment", "--comment", "kubernetes firewall for dropping marked packets",
		"-m", "mark", "--mark", dropMark,
		"-j", "DROP"); err != nil {
		glog.Errorf("Failed to ensure rule to drop packet marked by %v in %v chain %v: %v", KubeMarkDropChain, utiliptables.TableFilter, KubeFirewallChain, err)
		return
	}
	if _, err := iptClient.EnsureRule(utiliptables.Prepend, utiliptables.TableFilter, utiliptables.ChainOutput, "-j", string(KubeFirewallChain)); err != nil {
		glog.Errorf("Failed to ensure that %s chain %s jumps to %s: %v", utiliptables.TableFilter, utiliptables.ChainOutput, KubeFirewallChain, err)
		return
	}
	if _, err := iptClient.EnsureRule(utiliptables.Prepend, utiliptables.TableFilter, utiliptables.ChainInput, "-j", string(KubeFirewallChain)); err != nil {
		glog.Errorf("Failed to ensure that %s chain %s jumps to %s: %v", utiliptables.TableFilter, utiliptables.ChainInput, KubeFirewallChain, err)
		return
	}
	// Setup KUBE-MARK-MASQ rules
	masqueradeMark := getIPTablesMark(MasqueradeBit)
	if _, err := iptClient.EnsureChain(utiliptables.TableNAT, KubeMarkMasqChain); err != nil {
		glog.Errorf("Failed to ensure that %s chain %s exists: %v", utiliptables.TableNAT, KubeMarkMasqChain, err)
		return
	}
	if _, err := iptClient.EnsureChain(utiliptables.TableNAT, KubePostroutingChain); err != nil {
		glog.Errorf("Failed to ensure that %s chain %s exists: %v", utiliptables.TableNAT, KubePostroutingChain, err)
		return
	}
	if _, err := iptClient.EnsureRule(utiliptables.Append, utiliptables.TableNAT, KubeMarkMasqChain, "-j", "MARK", "--set-xmark", masqueradeMark); err != nil {
		glog.Errorf("Failed to ensure marking rule for %v: %v", KubeMarkMasqChain, err)
		return
	}
	if _, err := iptClient.EnsureRule(utiliptables.Prepend, utiliptables.TableNAT, utiliptables.ChainPostrouting,
		"-m", "comment", "--comment", "kubernetes postrouting rules", "-j", string(KubePostroutingChain)); err != nil {
		glog.Errorf("Failed to ensure that %s chain %s jumps to %s: %v", utiliptables.TableNAT, utiliptables.ChainPostrouting, KubePostroutingChain, err)
		return
	}
	if _, err := iptClient.EnsureRule(utiliptables.Append, utiliptables.TableNAT, KubePostroutingChain,
		"-m", "comment", "--comment", "kubernetes service traffic requiring SNAT",
		"-m", "mark", "--mark", masqueradeMark, "-j", "MASQUERADE"); err != nil {
		glog.Errorf("Failed to ensure SNAT rule for packets marked by %v in %v chain %v: %v", KubeMarkMasqChain, utiliptables.TableNAT, KubePostroutingChain, err)
		return
	}
}

// getIPTablesMark returns the fwmark given the bit
func getIPTablesMark(bit int32) string {
	value := 1 << uint(bit)
	return fmt.Sprintf("%#08x/%#08x", value, value)
}
