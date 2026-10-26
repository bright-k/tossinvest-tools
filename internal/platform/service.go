package platform

import (
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const ServiceLabel = "com.bright.tossinvest-tools"

func XMLString(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
func LaunchPlist(executable, root, backend, listen string) string {
	args := []string{executable, "serve", "--mode=readonly", "--backend=" + backend, "--app-dir=" + root, "--listen=" + listen}
	var a strings.Builder
	for _, arg := range args {
		fmt.Fprintf(&a, "<string>%s</string>", XMLString(arg))
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>Label</key><string>%s</string><key>ProgramArguments</key><array>%s</array><key>RunAtLoad</key><true/><key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict><key>ThrottleInterval</key><integer>30</integer><key>Umask</key><integer>63</integer></dict></plist>
`, ServiceLabel, a.String())
}
func ServicePath() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, "Library", "LaunchAgents", ServiceLabel+".plist")
}
func ServiceAction(action, plist string) error {
	domain := "gui/" + strconv.Itoa(os.Getuid())
	var args []string
	switch action {
	case "install":
		args = []string{"bootstrap", domain, plist}
	case "stop", "uninstall":
		args = []string{"bootout", domain + "/" + ServiceLabel}
	case "status":
		args = []string{"print", domain + "/" + ServiceLabel}
	default:
		return fmt.Errorf("invalid service action")
	}
	cmd := exec.Command("/bin/launchctl", args...)
	if e := cmd.Run(); e != nil {
		return fmt.Errorf("launchctl %s failed; inspect service status locally", action)
	}
	return nil
}
