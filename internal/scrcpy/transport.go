package scrcpy

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

func (t Tools) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, t.ADB, args...)
	cmd.WaitDelay = time.Second
	prepareCommand(cmd)
	return cmd
}

func (t Tools) Connect(ctx context.Context, address string) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if out, err := t.command(ctx, "connect", address).CombinedOutput(); err != nil {
		return fmt.Errorf("adb connect: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (t Tools) stream(parent context.Context, target Target, s *Session) error {
	core, err := t.load()
	if err != nil {
		return err
	}
	defer core.unload()
	decoder := core.open()
	if decoder == 0 {
		return fmt.Errorf("FFmpeg could not open the H.264 decoder")
	}
	defer core.close(decoder)
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	startup, timeout := context.WithTimeout(ctx, 20*time.Second)
	defer timeout()
	if target.Wireless {
		if err := t.Connect(startup, target.Address); err != nil {
			return err
		}
	}
	adb := func(c context.Context, args ...string) *exec.Cmd {
		return t.command(c, append([]string{"-s", target.Address}, args...)...)
	}
	cleanup := func(args ...string) {
		c, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		_ = adb(c, args...).Run()
	}
	var random [4]byte
	rand.Read(random[:])
	id := fmt.Sprintf("%08x", binary.BigEndian.Uint32(random[:])&0x7fffffff)
	jar := "/data/local/tmp/onrdp-scrcpy-" + id + ".jar"
	file := os.Getenv("SCRCPY_SERVER_PATH")
	if file == "" {
		file = filepath.Join(t.directory, "scrcpy-server")
	}
	defer cleanup("shell", "rm", "-f", jar)
	if out, err := adb(startup, "push", file, jar).CombinedOutput(); err != nil {
		return fmt.Errorf("adb push: %w: %s", err, strings.TrimSpace(string(out)))
	}
	portOut, err := adb(startup, "forward", "tcp:0", "localabstract:scrcpy_"+id).Output()
	if err != nil {
		return fmt.Errorf("adb forward: %w", err)
	}
	port := strings.TrimSpace(string(portOut))
	if n, err := strconv.ParseUint(port, 10, 16); err != nil || n == 0 {
		return fmt.Errorf("invalid forwarded port %q", port)
	}
	defer cleanup("forward", "--remove", "tcp:"+port)

	cmd := adb(ctx, "shell", "CLASSPATH="+jar, "app_process", "/", "com.genymobile.scrcpy.Server", Version,
		"scid="+id, "log_level=warn", "tunnel_forward=true", "audio=false", "send_device_meta=false", "video_codec=h264",
		"display_id="+strconv.Itoa(target.DisplayID), "clipboard_autosync="+strconv.FormatBool(s.clipboard))
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	var workers sync.WaitGroup
	workers.Go(func() { _ = cmd.Wait(); cancel() })
	defer func() { cancel(); workers.Wait() }()

	addr := net.JoinHostPort("127.0.0.1", port)
	video, err := dialVideo(startup, addr)
	if err != nil {
		return err
	}
	defer video.Close()
	control, err := (&net.Dialer{}).DialContext(startup, "tcp", addr)
	if err != nil {
		return err
	}
	defer control.Close()
	// Closing either channel unblocks all workers, including a stalled ReadFull.
	stop := context.AfterFunc(ctx, func() { video.Close(); control.Close() })
	defer stop()
	workers.Go(func() { s.receiveControl(control); cancel() })
	workers.Go(func() { s.writeControl(ctx, control); cancel() })
	return s.readVideo(video, core, decoder)
}

func dialVideo(ctx context.Context, addr string) (net.Conn, error) {
	for ctx.Err() == nil {
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
		if err == nil {
			// adb accepts before the device socket is listening and closes those attempts.
			// Only a zero handshake byte means this socket belongs to our server.
			deadline, _ := ctx.Deadline()
			conn.SetReadDeadline(deadline)
			stop := context.AfterFunc(ctx, func() { conn.Close() })
			var ready [1]byte
			_, err = io.ReadFull(conn, ready[:])
			if stop() && err == nil && ready[0] == 0 {
				conn.SetReadDeadline(time.Time{})
				return conn, nil
			}
			conn.Close()
		}
		select {
		case <-ctx.Done():
		case <-time.After(50 * time.Millisecond):
		}
	}
	return nil, ctx.Err()
}
