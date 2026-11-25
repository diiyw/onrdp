//go:build linux && !android && (amd64 || arm64)

package rustdesk

import (
	"net"
	"os"
	"testing"
	"time"

	"onrdp/internal/rdpclient"
)

// Exercise the pinned native ABI when the official core is available.
func TestNativeRustDeskDirectConnection(t *testing.T) {
	if os.Getenv("ONRDP_RUSTDESK_LIB") == "" {
		t.Skip("set ONRDP_RUSTDESK_LIB to exercise the RustDesk core")
	}
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := listener.SetDeadline(time.Now().Add(12 * time.Second)); err != nil {
		t.Fatal(err)
	}
	client := New(nil, Network{})
	defer client.Close()
	client.Connect(rdpclient.Config{Host: listener.Addr().String()})
	conn, err := listener.Accept()
	if err != nil {
		t.Fatalf("native connection: %v; client status: %+v", err, client.Status())
	}
	conn.Close()
}

func TestNativeRustDeskFramebuffer(t *testing.T) {
	target := os.Getenv("ONRDP_RUSTDESK_TEST_PEER")
	if target == "" || os.Getenv("ONRDP_RUSTDESK_LIB") == "" {
		t.Skip("requires an isolated RustDesk test peer and native core")
	}
	wake := make(chan struct{}, 1)
	client := New(func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	}, Network{})
	defer client.Close()
	client.Connect(rdpclient.Config{Host: target, Password: os.Getenv("ONRDP_RUSTDESK_TEST_PASSWORD")})
	timeout := time.After(20 * time.Second)
	for {
		status := client.Status()
		if status.State == rdpclient.StateClosed {
			t.Fatalf("RustDesk connection ended: %v", status.Error)
		}
		if status.State == rdpclient.StateConnected {
			regions, width, height, generation := client.Flush()
			if len(regions) != 1 || width <= 0 || height <= 0 || generation == 0 || len(regions[0].Pixels) != width*height*4 {
				t.Fatalf("invalid framebuffer: %dx%d, generation %d, %d regions", width, height, generation, len(regions))
			}
			t.Logf("received %dx%d desktop", width, height)
			if path := os.Getenv("ONRDP_RUSTDESK_TEST_INPUT"); path != "" {
				client.MouseMove(150, 100)
				client.MouseButton(0, true, 150, 100)
				client.MouseButton(0, false, 150, 100)
				client.KeyDown(rdpclient.Key{Scancode: 0x18})
				client.KeyUp(rdpclient.Key{Scancode: 0x18})
				client.SendText("nrdp")
				client.SendSequence([]rdpclient.Key{{Scancode: 0x1c}})
				for range 100 {
					if data, _ := os.ReadFile(path); string(data) == "onrdp" {
						return
					}
					time.Sleep(50 * time.Millisecond)
				}
				t.Fatal("the peer did not receive the expected keyboard input")
			}
			return
		}
		select {
		case <-wake:
		case <-timeout:
			t.Fatalf("RustDesk frame timeout: %+v", client.Status())
		}
	}
}
