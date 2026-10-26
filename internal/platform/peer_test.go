package platform

import (
	"testing"
)

func TestPeerUIDBoundary(t *testing.T) {
	if !SameUID(501, 501) || SameUID(501, 502) || SameUID(0, 501) {
		t.Fatal("peer UID authorization boundary")
	}
}
