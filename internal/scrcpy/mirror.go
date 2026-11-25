package scrcpy

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
	"unsafe"

	"onrdp/internal/i18n"
)

type library struct {
	unload func()
	abi    func() uint32
	open   func() uintptr
	decode func(uintptr, *byte, int32, **byte, *int32, *int32, *int32) int32
	close  func(uintptr)
}

type Session struct {
	cancel        context.CancelFunc
	finished      chan struct{}
	wake          func()
	outgoing      chan []byte
	clipboard     bool
	mu            sync.Mutex
	decoder       string
	err           error
	done          bool
	pending       bool
	front, spare  []byte
	width, height int
	clipText      string
	clipPending   bool
}

func (t Tools) Start(target Target, clipboard bool, wake func()) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Session{cancel: cancel, finished: make(chan struct{}), wake: wake, clipboard: clipboard, outgoing: make(chan []byte, 64)}
	go func() {
		err := t.stream(ctx, target, s)
		s.mu.Lock()
		if ctx.Err() == nil && err != nil {
			s.err = i18n.NewError("Android 投屏失败：%s", err.Error())
		}
		s.done = true
		s.mu.Unlock()
		cancel()
		s.wake()
		close(s.finished)
	}()
	return s
}

func (s *Session) Close() { s.cancel() }
func (s *Session) Wait()  { <-s.finished }
func (s *Session) State() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.done, s.err
}

// Frame lends the newest tightly packed RGBA frame until the next Frame call.
// The UI returns its previous buffer so the worker can reuse it.
func (s *Session) Frame(previous []byte) (pixels []byte, width, height int, decoder string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.pending {
		return nil, 0, 0, ""
	}
	pixels, s.front = s.front, previous
	s.pending = false
	return pixels, s.width, s.height, s.decoder
}

func (s *Session) readVideo(r net.Conn, core *library, handle uintptr) error {
	// A newly advertised display can accept both sockets before its encoder
	// produces a frame. An established static screen may stay idle indefinitely.
	if err := r.SetReadDeadline(time.Now().Add(20 * time.Second)); err != nil {
		return err
	}
	firstFrame := true
	var codec [4]byte
	if _, err := io.ReadFull(r, codec[:]); err != nil {
		return err
	}
	if string(codec[:]) != "h264" {
		return fmt.Errorf("unexpected video codec %x", codec)
	}
	var header [12]byte
	var packet []byte
	var prefix int
	const maxPacket = 32 << 20
	for {
		if _, err := io.ReadFull(r, header[:]); err != nil {
			return err
		}
		if header[0]&0x80 != 0 {
			prefix = 0
			continue
		} // session metadata on rotation or resize
		n := binary.BigEndian.Uint32(header[8:])
		if n == 0 || n > uint32(maxPacket-prefix) {
			return fmt.Errorf("invalid video packet length %d", n)
		}
		size := prefix + int(n)
		if cap(packet) < size {
			next := make([]byte, size)
			copy(next, packet[:prefix])
			packet = next
		} else {
			packet = packet[:size]
		}
		if _, err := io.ReadFull(r, packet[prefix:]); err != nil {
			return err
		}
		if header[0]&0x40 != 0 {
			prefix = size
			continue
		} // prepend codec configuration to the next access unit
		prefix = 0
		var pixels *byte
		var width, height, stride int32
		code := core.decode(handle, &packet[0], int32(size), &pixels, &width, &height, &stride)
		if code == 0 {
			continue
		}
		backends := [...]string{"", "software", "VA-API", "D3D11VA", "VideoToolbox"}
		if code < 0 || code >= int32(len(backends)) {
			return fmt.Errorf("FFmpeg decode error %d", code)
		}
		if pixels == nil || width <= 0 || height <= 0 || width > 65535 || height > 65535 || int64(width)*int64(height) > 8192*8192 || stride < width*4 || int64(stride)*int64(height) > 1<<30 {
			return fmt.Errorf("invalid decoded frame %dx%d", width, height)
		}
		if firstFrame {
			if err := r.SetReadDeadline(time.Time{}); err != nil {
				return err
			}
			firstFrame = false
		}
		row := int(width) * 4
		data := unsafe.Slice(pixels, int(stride)*int(height))
		buf := s.spare
		length := row * int(height)
		if cap(buf) < length {
			buf = make([]byte, length)
		} else {
			buf = buf[:length]
		}
		if row == int(stride) {
			copy(buf, data)
		} else {
			for y := 0; y < int(height); y++ {
				copy(buf[y*row:(y+1)*row], data[y*int(stride):])
			}
		}
		s.mu.Lock()
		s.front, s.spare = buf, s.front
		s.width, s.height, s.decoder = int(width), int(height), backends[code]
		notify := !s.pending
		s.pending = true
		s.mu.Unlock()
		if notify {
			s.wake()
		}
	}
}
