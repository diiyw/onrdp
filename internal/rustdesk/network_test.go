package rustdesk

import "testing"

func TestNetworkTarget(t *testing.T) {
	for _, tt := range []struct {
		id      string
		network Network
		want    string
	}{
		{"123 456", Network{}, "123456"},
		{"192.0.2.1:21118", Network{}, "192.0.2.1:21118"},
		{"Office-PC", Network{ForceRelay: true}, "Office-PC"},
		{"123", Network{IDServer: "hbbs.example:41116", RelayServer: "relay.example:42117", Key: "+/a="}, "123@hbbs.example:41116?key=+/a="},
		{"123", Network{IDServer: "[2001:db8::1]"}, "123@2001:db8::1"},
		{"123", Network{IDServer: "[2001:db8::1]:41116"}, "123@[2001:db8::1]:41116"},
	} {
		got, err := tt.network.Target(tt.id)
		if err != nil || got != tt.want {
			t.Errorf("Target(%q, %+v) = %q, %v; want %q", tt.id, tt.network, got, err, tt.want)
		}
	}
	for _, n := range []Network{
		{IDServer: "https://hbbs.example"}, {RelayServer: "relay.example:65536"},
		{RelayServer: "relay.example:0"}, {RelayServer: "[2001:db8::1"},
		{IDServer: "server?key=other"}, {Key: "a"}, {IDServer: "server", Key: "a&key=b"},
	} {
		if _, err := n.Target("123"); err == nil {
			t.Errorf("accepted invalid network settings %+v", n)
		}
	}
	if _, err := (Network{ForceRelay: true}).Target("192.0.2.1"); err == nil {
		t.Error("force relay with a direct IP target must prompt for a device ID")
	}
}

func TestCopyRelayAddressWithoutIDServer(t *testing.T) {
	n := Network{RelayServer: "relay.example:42117"}
	if text, err := n.ClipboardText(); err != nil || text != n.RelayServer {
		t.Fatalf("ClipboardText() = %q, %v", text, err)
	}
}
