package platform

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
)

func PrivateDir(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	f, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if !f.IsDir() || f.Mode()&os.ModeSymlink != 0 || f.Mode().Perm() != 0700 {
		return fmt.Errorf("directory must be private 0700 and not symlink")
	}
	st, ok := f.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != os.Getuid() {
		return fmt.Errorf("directory owner mismatch")
	}
	return nil
}
func CheckFile(path string, mode os.FileMode) error {
	f, e := os.Lstat(path)
	if e != nil {
		return e
	}
	st, ok := f.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != os.Getuid() || !f.Mode().IsRegular() || f.Mode().Perm() != mode {
		return fmt.Errorf("file must be owned regular %04o without symlink", mode)
	}
	return nil
}
func AtomicJSON(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	return Atomic(path, b)
}
func Atomic(path string, b []byte) error {
	if err := PrivateDir(filepath.Dir(path)); err != nil {
		return err
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".pending-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	f.Close()
	if e != nil {
		return e
	}
	return os.Rename(f.Name(), path)
}

type Lock struct{ f *os.File }

func Acquire(path string) (*Lock, error) {
	if _, e := os.Lstat(path); e == nil {
		if e = CheckFile(path, 0600); e != nil {
			return nil, e
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		return nil, fmt.Errorf("server already running for app root")
	}
	return &Lock{f}, nil
}
func (l *Lock) Close() { _ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN); _ = l.f.Close() }
func ValidateListen(addr string) error {
	h, p, e := net.SplitHostPort(addr)
	if e != nil || p == "0" || p == "" || h != "127.0.0.1" {
		return fmt.Errorf("listen must be 127.0.0.1 with fixed port")
	}
	return nil
}

func SameUID(peer, server uint32) bool { return peer == server }
