//go:build darwin

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
		cred, err := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		ok = err == nil && SameUID(cred.Uid, uint32(os.Getuid()))
	})
	return e == nil && ok
}
