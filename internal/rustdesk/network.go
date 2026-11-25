package rustdesk

import (
	"encoding/json"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"onrdp/internal/i18n"
)

// Network holds the optional server settings for one saved RustDesk peer.
type Network struct {
	IDServer    string `json:"idServer,omitempty"`
	RelayServer string `json:"relayServer,omitempty"`
	Key         string `json:"key,omitempty"`
	ForceRelay  bool   `json:"forceRelay,omitempty"`
}

var serverLabel = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

func validServer(address string) bool {
	if address == "" {
		return true
	}
	if ip, err := netip.ParseAddr(address); err == nil {
		return ip.Zone() == ""
	}
	if strings.HasPrefix(address, "[") && strings.HasSuffix(address, "]") {
		ip, err := netip.ParseAddr(address[1 : len(address)-1])
		return err == nil && ip.Is6() && ip.Zone() == ""
	}
	if strings.HasPrefix(address, "[") {
		ap, err := netip.ParseAddrPort(address)
		return err == nil && ap.Addr().Is6() && ap.Addr().Zone() == "" && ap.Port() != 0
	}
	host, port, hasPort := strings.Cut(address, ":")
	if hasPort {
		n, err := strconv.ParseUint(port, 10, 16)
		if err != nil || n == 0 {
			return false
		}
	}
	host = strings.TrimSuffix(host, ".")
	if len(host) > 253 {
		return false
	}
	for label := range strings.SplitSeq(host, ".") {
		if !serverLabel.MatchString(label) {
			return false
		}
	}
	return true
}

// The core adds the default port to bare IPv6 literals. It treats a leading
// bracket as an endpoint that already has a port.
func coreServer(address string) string {
	if strings.HasPrefix(address, "[") && strings.HasSuffix(address, "]") {
		return address[1 : len(address)-1]
	}
	return address
}

func (n Network) validate() error {
	if !validServer(n.IDServer) {
		return i18n.NewError("请输入有效的 ID 服务器地址")
	}
	if !validServer(n.RelayServer) {
		return i18n.NewError("请输入有效的中继服务器地址")
	}
	if n.IDServer == "" && n.Key != "" {
		return i18n.NewError("请填写 ID 服务器地址")
	}
	if strings.ContainsAny(n.Key, "@?&") || strings.IndexFunc(n.Key, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0 {
		return i18n.NewError("服务器公钥格式不正确")
	}
	return nil
}

// Target uses the core's per-session ID@server?key= syntax. The controlled
// peer advertises the hbbr endpoint during rendezvous.
func (n Network) Target(id string) (string, error) {
	target, err := NormalizeID(id)
	if err != nil {
		return "", err
	}
	if err := n.validate(); err != nil {
		return "", err
	}
	if n != (Network{}) && isIPEndpoint(target) {
		return "", i18n.NewError("自定义服务器和中继连接需要 RustDesk 设备 ID")
	}
	if n.IDServer != "" {
		target += "@" + coreServer(n.IDServer)
		if n.Key != "" {
			target += "?key=" + n.Key
		}
	}
	return target, nil
}

// ClipboardText returns settings for RustDesk's Import Server Config, or just
// the relay address for the remote device's network settings.
func (n Network) ClipboardText() (string, error) {
	if err := n.validate(); err != nil {
		return "", err
	}
	if n.IDServer == "" {
		if n.RelayServer == "" {
			return "", i18n.NewError("请输入有效的中继服务器地址")
		}
		return coreServer(n.RelayServer), nil
	}
	data, err := json.Marshal(map[string]string{"host": coreServer(n.IDServer), "relay": coreServer(n.RelayServer), "key": n.Key})
	return string(data), err
}
