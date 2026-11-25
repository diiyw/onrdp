package scrcpy

import "testing"

func TestEndpoint(t *testing.T) {
	for input, want := range map[string]string{
		" phone.local ": "phone.local:5555", "192.0.2.42:39001": "192.0.2.42:39001",
		"[2001:db8::1]:4567": "[2001:db8::1]:4567", "2001:db8::2": "[2001:db8::2]:5555",
		"[::1]": "[::1]:5555", "[::1": "", "": "", ":5555": "", "host:65536": "",
		"host:0": "", "+phone": "", "-x": "", "two words": "", "host:pair": "",
	} {
		got, err := Endpoint(input)
		if got != want || (err == nil) != (want != "") {
			t.Errorf("Endpoint(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
}
