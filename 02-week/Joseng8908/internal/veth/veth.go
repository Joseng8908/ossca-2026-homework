package veth

import (
	"os"
	"runtime"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

type Config struct {
	HostIfname string
	PeerIfname string
	HostIP     string
	PeerIP     string
}

func Veth(name string, cfg *Config) error {
	veth := &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{
			Name: cfg.HostIfname,
		},
		PeerName: cfg.PeerIfname,
	}

	if err := netlink.LinkAdd(veth); err != nil {
		return err
	}

	fd, err := os.Open("/var/run/netns/" + name)
	if err != nil {
		return err
	}
	defer fd.Close()

	hostAddr, err := netlink.ParseAddr(cfg.HostIP)
	if err != nil {
		return err
	}
	netlink.AddrAdd(veth, hostAddr)

	netlink.LinkSetUp(veth)

	peer, _ := netlink.LinkByName(cfg.PeerIfname)
	netlink.LinkSetNsFd(peer, int(fd.Fd()))

	return ConfigPeerNs(name, cfg.PeerIfname, cfg.PeerIP)
}

func ConfigPeerNs(name string, peerIfname string, peerIP string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	originNs, err := os.Open("/proc/self/ns/net")
	if err != nil {
		return err
	}
	defer originNs.Close()

	nsFd, err := os.Open("/var/run/netns/" + name)
	if err != nil {
		return err
	}
	defer nsFd.Close()

	unix.Setns(int(nsFd.Fd()), unix.CLONE_NEWNET)
	defer unix.Setns(int(originNs.Fd()), unix.CLONE_NEWNET)

	peer, err := netlink.LinkByName(peerIfname)
	if err != nil {
		return err
	}
	peerAddr, err := netlink.ParseAddr(peerIP)
	if err != nil {
		return err
	}
	if err := netlink.AddrAdd(peer, peerAddr); err != nil {
		return err
	}
	netlink.LinkSetUp(peer)

	lo, _ := netlink.LinkByName("lo")
	netlink.LinkSetUp(lo)

	return nil
}
