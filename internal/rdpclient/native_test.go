package rdpclient

import (
	"net"
	"strconv"
	"testing"
	"time"
)

func TestNativeInputQueuePreservesTransitions(t *testing.T) {
	s := newNativeSession(New(nil))
	s.SendMouse(1, 2, 0)
	s.SendMouse(3, 4, 0)
	s.SendMouse(3, 4, pduButton1|pduDown)
	s.SendMouse(5, 6, 0)
	s.SendKeyboardExtended(0x4b, true, true)
	s.SendKeyboardExtended(0x4b, false, true)
	s.SendUnicode(0x4e2d, true)
	s.SendMouseWheel(5, 6, -3, false)
	s.SendMouseWheel(5, 6, 1, true)
	want := []inputEvent{
		{kind: 3, flags: 0x0800, x: 3, y: 4},
		{kind: 3, flags: pduButton1 | pduDown, x: 3, y: 4},
		{kind: 3, flags: 0x0800, x: 5, y: 6},
		{kind: 1, flags: 0x0100, code: 0x4b},
		{kind: 1, flags: 0x8100, code: 0x4b},
		{kind: 2, code: 0x4e2d},
		{kind: 3, flags: 0x0310, x: 5, y: 6},
		{kind: 3, flags: 0x0388, x: 5, y: 6},
		{kind: 3, flags: 0x0478, x: 5, y: 6},
	}
	if len(s.queue) != len(want) {
		t.Fatalf("queue length=%d want %d", len(s.queue), len(want))
	}
	for i, e := range want {
		if s.queue[i] != e {
			t.Fatalf("event %d=%+v want %+v", i, s.queue[i], e)
		}
	}
	s.Close()
	if err := s.SendMouse(0, 0, 0); err != errSessionClosed {
		t.Fatalf("closed input accepted: %v", err)
	}
}

// An RDP peer that accepts TCP but never negotiates must not keep a cancelled
// native worker alive, or resurrect StateConnected after the UI closed it.
func TestCloseAbortsNativeHandshake(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			accepted <- conn
		}
	}()
	_, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	c := New(nil)
	c.Connect(Config{Host: "127.0.0.1", Port: port, Username: "test", Password: "test", Width: 640, Height: 480, Reconnect: true})
	c.mu.Lock()
	s := c.rdp.(*nativeSession)
	c.mu.Unlock()
	select {
	case conn := <-accepted:
		defer conn.Close()
	case <-time.After(5 * time.Second):
		c.Close()
		t.Fatal("no native TCP connection")
	}
	c.Close()
	select {
	case <-s.stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled handshake did not stop")
	}
	if c.Status().State != StateClosed {
		t.Fatal("cancelled connection resurrected")
	}
}

func TestCloseBeforeNativeWorkerStarts(t *testing.T) {
	c := New(nil)
	s := newNativeSession(c)
	c.rdp = s
	c.state = StateConnecting
	c.Close()
	go s.run(Config{Host: "127.0.0.1", Port: 1, Reconnect: true})
	select {
	case <-s.stopped:
	case <-time.After(time.Second):
		t.Fatal("cancelled worker attempted a connection")
	}
	if c.Status().State != StateClosed {
		t.Fatal("cancelled worker changed status")
	}
}

func TestDesktopResolutionSurvivesInputQueueReset(t *testing.T) {
	c := New(nil)
	s := newNativeSession(c)
	c.rdp, c.state = s, StateConnecting
	c.ResizeDesktop(6017, 3384)
	s.SendMouse(1, 2, 0)
	s.queueMu.Lock()
	s.queue = nil // reconnect drops old input, but must retain the display request
	w, h := s.desktopWidth, s.desktopHeight
	s.queueMu.Unlock()
	if w != 6016 || h != 3384 {
		t.Fatalf("pending desktop=%dx%d, want 6016x3384", w, h)
	}
	c.Close()
	c.ResizeDesktop(1920, 1080)
	if s.desktopWidth != w || s.desktopHeight != h {
		t.Fatal("closed session accepted a resolution change")
	}
}
