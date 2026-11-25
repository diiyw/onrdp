package scrcpy

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"time"
	"unicode/utf8"
)

var wire = binary.BigEndian

func (s *Session) writeControl(ctx context.Context, w net.Conn) {
	for {
		select {
		case <-ctx.Done():
			return
		case packet := <-s.outgoing:
			w.SetWriteDeadline(time.Now().Add(2 * time.Second))
			if _, err := w.Write(packet); err != nil {
				return
			}
		}
	}
}

func (s *Session) receiveControl(r io.Reader) {
	var kind [1]byte
	for {
		if _, err := io.ReadFull(r, kind[:]); err != nil {
			return
		}
		if kind[0] == 1 { // clipboard acknowledgement
			if _, err := io.CopyN(io.Discard, r, 8); err != nil {
				return
			}
			continue
		}
		if kind[0] != 0 {
			return
		}
		var n uint32
		if err := binary.Read(r, wire, &n); err != nil || n > 1<<18 {
			return
		}
		data := make([]byte, n)
		if _, err := io.ReadFull(r, data); err != nil {
			return
		}
		if s.clipboard {
			s.mu.Lock()
			s.clipText, s.clipPending = string(data), true
			s.mu.Unlock()
			s.wake()
		}
	}
}

func (s *Session) TakeClipboard() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	text, ok := s.clipText, s.clipPending
	s.clipText, s.clipPending = "", false
	return text, ok
}

func (s *Session) send(packet []byte, motion bool) {
	select {
	case s.outgoing <- packet:
	default:
		// Motion is expendable. A stalled channel must not leave a button
		// held indefinitely or block the host UI on a socket write.
		if !motion {
			s.cancel()
		}
	}
}

func (s *Session) Key(code, mods uint32, down bool) {
	packet := make([]byte, 14)
	if !down {
		packet[1] = 1
	}
	wire.PutUint32(packet[2:], code)
	wire.PutUint32(packet[10:], mods)
	s.send(packet, false)
}

// TypeText sends a host text commit. Android's virtual key map cannot represent
// CJK or emoji, so paste the whole commit (including any ASCII) in that case.
func (s *Session) TypeText(text string) {
	paste := strings.ContainsFunc(text, func(r rune) bool { return r >= utf8.RuneSelf })
	s.sendText(text, paste)
}

func (s *Session) Paste(text string) {
	if s.clipboard {
		s.sendText(text, true)
	}
}

func (s *Session) sendText(text string, paste bool) {
	header, limit := 5, 300
	if paste {
		header, limit = 14, (1<<18)-14
	}
	for len(text) > 0 {
		n := min(len(text), limit)
		for n < len(text) && !utf8.RuneStart(text[n]) {
			n--
		}
		packet := make([]byte, header+n)
		packet[0] = 1
		if paste {
			packet[0], packet[9] = 9, 1
		}
		wire.PutUint32(packet[header-4:], uint32(n))
		copy(packet[header:], text[:n])
		s.send(packet, false)
		text = text[n:]
	}
}

func position(buf []byte, x, y, w, h int) {
	wire.PutUint32(buf, uint32(x))
	wire.PutUint32(buf[4:], uint32(y))
	wire.PutUint16(buf[8:], uint16(w))
	wire.PutUint16(buf[10:], uint16(h))
}

func (s *Session) Touch(action byte, x, y, w, h int) {
	msg := make([]byte, 32)
	msg[0], msg[1] = 2, action
	wire.PutUint64(msg[2:], ^uint64(0)) // scrcpy's mouse pointer id
	position(msg[10:], x, y, w, h)
	if action != 1 && action != 7 {
		wire.PutUint16(msg[22:], 65535)
		wire.PutUint32(msg[28:], 1)
	}
	if action == 0 || action == 1 {
		wire.PutUint32(msg[24:], 1)
	}
	s.send(msg, action == 2 || action == 7)
}

func (s *Session) Scroll(x, y, w, h, steps int, horizontal bool) {
	msg := make([]byte, 21)
	msg[0] = 3
	position(msg[1:], x, y, w, h)
	off := 15
	if horizontal {
		off = 13
	}
	wire.PutUint16(msg[off:], uint16(max(-32768, min(32767, steps*2048))))
	s.send(msg, true)
}
