package scrcpy

import (
	"net"
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	"onrdp/internal/i18n"
)

var dnsLabel = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

// Endpoint accepts an ADB host and optional port, including IPv6 literals.
func Endpoint(value string) (string, error) {
	host, port := strings.TrimSpace(value), "5555"
	invalid := i18n.NewError("请输入有效的 ADB 地址和端口")
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
		if ip, err := netip.ParseAddr(host); err != nil || !ip.Is6() {
			return "", invalid
		}
	} else if _, err := netip.ParseAddr(host); err != nil && strings.Contains(host, ":") {
		host, port, err = net.SplitHostPort(host)
		if err != nil {
			return "", invalid
		}
	}
	if _, err := netip.ParseAddr(host); err != nil {
		for label := range strings.SplitSeq(strings.TrimSuffix(host, "."), ".") {
			if !dnsLabel.MatchString(label) {
				return "", invalid
			}
		}
	}
	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil || number == 0 {
		return "", invalid
	}
	return net.JoinHostPort(host, strconv.FormatUint(number, 10)), nil
}

type Target struct {
	Address   string
	DisplayID int  // Zero selects the default display.
	Wireless  bool // Connect to Address over TCP before selecting it.
}
