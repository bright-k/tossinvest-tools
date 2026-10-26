//go:build !darwin || !cgo

package platform

import (
	"context"
	"github.com/bright-k/tossinvest-tools/internal/broker/toss"
	"github.com/bright-k/tossinvest-tools/internal/fault"
)

type TossSecrets struct{}

func (TossSecrets) Read(context.Context) (toss.Credentials, error) {
	return toss.Credentials{}, fault.New("NOT_READY", "macOS Keychain requires darwin cgo build")
}
func SaveToss(toss.Credentials) error { return fault.New("NOT_READY", "macOS Keychain unavailable") }
func ReadMCP() (string, error)        { return "", fault.New("NOT_READY", "macOS Keychain unavailable") }
func SaveMCP(string) error            { return fault.New("NOT_READY", "macOS Keychain unavailable") }
