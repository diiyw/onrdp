// Package scrcpy connects Android devices using the bundled native library.
package scrcpy

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"onrdp/internal/i18n"
)

const Version = "5.0.1"

type Tools struct {
	ADB       string
	directory string
	library   string
}

func Resolve() (Tools, error) {
	exe, err := os.Executable()
	if err != nil {
		return Tools{}, err
	}
	dir := filepath.Dir(exe)
	if runtime.GOOS == "darwin" && filepath.Base(dir) == "MacOS" {
		dir = filepath.Join(dir, "..", "Resources")
	}
	dir = filepath.Join(dir, "tools", "scrcpy")
	library := os.Getenv("ONRDP_SCRCPY_LIBRARY")
	if library != "" {
		library, err = filepath.Abs(library)
		if err != nil {
			return Tools{}, err
		}
		dir = filepath.Dir(library)
	} else {
		name := "libonrdp-scrcpy.so"
		switch runtime.GOOS {
		case "darwin":
			name = "libonrdp-scrcpy.dylib"
		case "windows":
			name = "libonrdp-scrcpy.dll"
		}
		library = filepath.Join(dir, name)
	}

	adb := os.Getenv("ADB")
	if adb == "" {
		adb = siblingOrPath(dir, "adb")
	}
	adb, err = exec.LookPath(adb)
	if err != nil {
		return Tools{}, i18n.NewError("找不到 adb，请安装 Android SDK Platform-Tools。")
	}
	adb, err = filepath.Abs(adb)
	if err != nil {
		return Tools{}, err
	}
	return Tools{ADB: adb, directory: dir, library: library}, nil
}

func siblingOrPath(dir, name string) string {
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return name
}

type Device struct {
	Serial, Model, State string
}

func (t Tools) InstallAPK(ctx context.Context, serial, path string) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return i18n.NewError("请选择 APK 文件")
	}
	output, err := t.command(ctx, "-s", serial, "install", "-r", path).CombinedOutput()
	if err != nil {
		if message := strings.TrimSpace(string(output)); message != "" {
			return errors.New(message)
		}
	}
	return err
}

type Display struct {
	ID      int
	Name    string
	Focused bool // Global Android input focus, when reported by the device.
	// App identifies the foreground standard activity. TaskID is nonzero only
	// for a visible root containing exactly that single application task.
	App        string
	TaskID     int
	TasksKnown bool
	Home       bool
	capture    bool
	mumuScreen bool
	hasRoot    bool
}

func (t Tools) Devices(ctx context.Context) ([]Device, error) {
	cmd := exec.CommandContext(ctx, t.ADB, "devices", "-l")
	cmd.WaitDelay = time.Second
	prepareCommand(cmd)
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) && len(exit.Stderr) > 0 {
			return nil, errors.New(strings.TrimSpace(string(exit.Stderr)))
		}
		return nil, err
	}
	var devices []Device
	_, rows, ok := strings.Cut(string(out), "List of devices attached")
	if !ok {
		return nil, i18n.NewError("无法读取 ADB 设备列表")
	}
	for line := range strings.SplitSeq(rows, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		device := Device{Serial: fields[0], Model: fields[0], State: fields[1]}
		for _, field := range fields[2:] {
			if model, ok := strings.CutPrefix(field, "model:"); ok {
				device.Model = strings.ReplaceAll(model, "_", " ")
			}
		}
		devices = append(devices, device)
	}
	return devices, nil
}

func (t Tools) Displays(ctx context.Context, serial string) ([]Display, error) {
	displays, err := t.displaySnapshot(ctx, serial)
	return visibleDisplays(displays), err
}

func visibleDisplays(displays []Display) []Display {
	return slices.DeleteFunc(displays, func(d Display) bool { return d.capture })
}

func (t Tools) displaySnapshot(ctx context.Context, serial string) ([]Display, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, t.ADB, "-s", serial, "shell", "cmd", "display", "get-displays")
	cmd.WaitDelay = time.Second
	prepareCommand(cmd)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var displays []Display
	for line := range strings.SplitSeq(string(output), "\n") {
		line, ok := strings.CutPrefix(strings.TrimSpace(line), "Display id ")
		if !ok {
			continue
		}
		number, info, ok := strings.Cut(line, ":")
		id, err := strconv.Atoi(number)
		if !ok || err != nil || id < 0 {
			continue
		}
		var name string
		if _, info, ok := strings.Cut(info, `DisplayInfo{"`); ok {
			name, _, _ = strings.Cut(info, `"`)
		}
		displays = append(displays, Display{ID: id, Name: name,
			capture:    name == "scrcpy" && strings.Contains(info, "FLAG_PRIVATE") && strings.Contains(info, "type VIRTUAL"),
			mumuScreen: strings.HasPrefix(name, "mumuscreen") && strings.Contains(info, "type INTERNAL"),
		})
	}
	if len(displays) == 0 {
		return nil, i18n.NewError("无法读取显示屏列表")
	}
	tasks, err := t.command(ctx, "-s", serial, "shell", "cmd", "activity", "stack", "list").Output()
	if err == nil {
		applyTasks(displays, string(tasks))
	}
	return displays, nil
}

// Read only the global focus line. Individual displays may each have a
// focused app even though just one is receiving keyboard input.
func (t Tools) FocusedDisplay(ctx context.Context, serial string) int {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	const query = "dumpsys input | grep -m1 'FocusedDisplayId:' || dumpsys window | grep -m1 'mTopFocusedDisplayId='"
	output, err := t.command(ctx, "-s", serial, "shell", query).Output()
	if err == nil {
		line := strings.TrimSpace(string(output))
		for _, prefix := range []string{"FocusedDisplayId:", "mTopFocusedDisplayId="} {
			if value, ok := strings.CutPrefix(line, prefix); ok {
				if id, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && id >= 0 {
					return id
				}
			}
		}
	}
	return -1
}
