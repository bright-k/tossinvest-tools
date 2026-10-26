//go:build linux

package platform

import (
	"golang.org/x/sys/unix"
	"net"
	"os"
)

func PeerAllowed(c *net.UnixConn) bool {
	raw, e := c.SyscallConn()
	if e != nil {
		return false
	}
	ok := false
	e = raw.Control(func(fd uintptr) {
		cred, err := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		ok = err == nil && SameUID(cred.Uid, uint32(os.Getuid()))
	})
	return e == nil && ok
}
