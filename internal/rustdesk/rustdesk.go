// Package rustdesk embeds the RustDesk remote desktop core.
package rustdesk

import (
	"net"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"onrdp/internal/i18n"
)

var (
	deviceID = regexp.MustCompile(`^(?:[0-9]+|[A-Za-z][A-Za-z0-9_-]*)$`)
	ErrID    = i18n.NewError("请输入有效的 RustDesk 设备 ID 或 IP 地址")
)

// NormalizeID accepts typed IDs and the grouped numeric IDs shown by RustDesk.
func NormalizeID(id string) (string, error) {
	id = strings.TrimSpace(id)
	if strings.IndexFunc(id, func(r rune) bool { return (r < '0' || r > '9') && !unicode.IsSpace(r) }) == -1 {
		id = strings.Join(strings.Fields(id), "")
	}
	if !deviceID.MatchString(id) && !isIPEndpoint(id) {
		return "", ErrID
	}
	return id, nil
}

func isIPEndpoint(id string) bool {
	if _, err := netip.ParseAddr(id); err == nil {
		return true
	}
	host, port, err := net.SplitHostPort(id)
	if err != nil {
		return false
	}
	_, ipErr := netip.ParseAddr(host)
	number, portErr := strconv.ParseUint(port, 10, 16)
	return ipErr == nil && portErr == nil && number > 0
}
