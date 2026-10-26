package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrivatePathsSymlinksAndLock(t *testing.T) {
	root := filepath.Join(t.TempDir(), "private")
	if e := PrivateDir(root); e != nil {
		t.Fatal(e)
	}
	link := root + "-link"
	if e := os.Symlink(root, link); e != nil {
		t.Fatal(e)
	}
	if PrivateDir(link) == nil {
		t.Fatal("symlink app root accepted")
	}
	file := filepath.Join(root, "connection.json")
	if e := Atomic(file, []byte(`{"mock":true}`)); e != nil {
		t.Fatal(e)
	}
	if e := CheckFile(file, 0600); e != nil {
		t.Fatal(e)
	}
	os.Chmod(file, 0644)
	if CheckFile(file, 0600) == nil {
		t.Fatal("world readable credential")
	}
	lock, e := Acquire(filepath.Join(root, "server.lock"))
	if e != nil {
		t.Fatal(e)
	}
	if other, e := Acquire(filepath.Join(root, "server.lock")); e == nil {
		other.Close()
		t.Fatal("second lock accepted")
	}
	lock.Close()
}
