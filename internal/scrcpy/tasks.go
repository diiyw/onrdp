package scrcpy

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"onrdp/internal/i18n"
)

// Android prints root tasks front to back. Only the first visible root on a
// display can identify its foreground app. Never remove a root that groups
// other tasks (split screen, a launcher, or an organizer).
func applyTasks(displays []Display, listing string) {
	type rootTask struct {
		id, display, count, child int
		standard, home, visible   bool
		app                       string
	}
	seen := make(map[int]bool)
	var root rootTask
	recognized := false
	finish := func() {
		if root.id <= 0 || root.display < 0 {
			return
		}
		for i := range displays {
			d := &displays[i]
			if d.ID != root.display {
				continue
			}
			d.hasRoot = true
			if !root.visible || seen[root.display] {
				return
			}
			seen[root.display] = true
			d.Home = root.home
			if !root.standard {
				return
			}
			d.App = root.app
			if root.count == 1 && root.child == root.id && root.app != "" {
				d.TaskID = root.id
			}
		}
	}
	for line := range strings.SplitSeq(listing, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "RootTask id="), strings.HasPrefix(line, "Stack id="):
			finish()
			root = rootTask{display: -1}
			for field := range strings.FieldsSeq(line) {
				if value, ok := strings.CutPrefix(field, "id="); ok {
					root.id, _ = strconv.Atoi(value)
				}
				if value, ok := strings.CutPrefix(field, "displayId="); ok {
					if id, err := strconv.Atoi(value); err == nil {
						root.display = id
					}
				}
			}
			recognized = recognized || (root.id > 0 && root.display >= 0)
		case strings.HasPrefix(line, "configuration="):
			root.standard = strings.Contains(line, "mActivityType=standard ")
			root.home = strings.Contains(line, "mActivityType=home ")
		case strings.HasPrefix(line, "taskId="):
			root.count++
			id, _, _ := strings.Cut(strings.TrimPrefix(line, "taskId="), ":")
			root.child, _ = strconv.Atoi(id)
			if strings.Contains(" "+line+" ", " visible=true ") {
				root.visible = true
				if _, component, ok := strings.Cut(line, "topActivity=ComponentInfo{"); ok {
					if pkg, _, ok := strings.Cut(component, "/"); ok {
						root.app = pkg
					}
				}
			}
		}
	}
	finish()
	for i := range displays {
		displays[i].TasksKnown = recognized
	}
}

type taskMove struct {
	source, task, destination int
	pkg                       string
}

// MuMu can launch onto scrcpy's private mirror just before allocating its
// own display. Closing the mirror at that point also destroys the app task.
func misplacedMuMuTask(displays, beforeTap []Display) (taskMove, bool) {
	var capture, target *Display
	for i := range displays {
		d := &displays[i]
		if d.capture {
			if capture != nil {
				return taskMove{}, false
			}
			capture = d
		}
		if d.mumuScreen && !slices.ContainsFunc(beforeTap, func(old Display) bool { return old.ID == d.ID }) {
			if target != nil {
				return taskMove{}, false
			}
			target = d
		}
	}
	if capture == nil || capture.TaskID <= 0 || target == nil || target.ID <= capture.ID || !target.TasksKnown || target.hasRoot {
		return taskMove{}, false
	}
	return taskMove{source: capture.ID, task: capture.TaskID, destination: target.ID, pkg: capture.App}, true
}

// DisplaysAfterLauncherTap rechecks the task and both displays before moving
// anything. The next mirror must see the moved task before the old one closes.
func (t Tools) DisplaysAfterLauncherTap(ctx context.Context, serial string, beforeTap []Display) ([]Display, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	displays, err := t.displaySnapshot(ctx, serial)
	move, ok := misplacedMuMuTask(displays, beforeTap)
	if err != nil || !ok {
		return visibleDisplays(displays), err
	}
	displays, err = t.displaySnapshot(ctx, serial)
	confirmed, ok := misplacedMuMuTask(displays, beforeTap)
	if err != nil || !ok || confirmed != move {
		return visibleDisplays(displays), err
	}
	out, err := t.command(ctx, "-s", serial, "shell", "cmd", "activity", "display", "move-stack", strconv.Itoa(move.task), strconv.Itoa(move.destination)).CombinedOutput()
	if err != nil || len(strings.TrimSpace(string(out))) > 0 {
		return nil, fmt.Errorf("adb move-stack: %v %s", err, strings.TrimSpace(string(out)))
	}
	return t.Displays(ctx, serial)
}

func (t Tools) ExitApp(ctx context.Context, serial string, target Display) error {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	// Recheck the display and task at click time. The emulator may already
	// have replaced the task since the last display scan.
	displays, err := t.Displays(ctx, serial)
	if err != nil {
		return err
	}
	for _, d := range displays {
		if d.ID != target.ID || d.App == "" || d.App != target.App || d.TaskID <= 0 || d.TaskID != target.TaskID {
			continue
		}
		out, err := t.command(ctx, "-s", serial, "shell", "cmd", "activity", "stack", "remove", strconv.Itoa(d.TaskID)).CombinedOutput()
		if err != nil {
			return fmt.Errorf("adb: %w: %s", err, strings.TrimSpace(string(out)))
		}
		if detail := strings.TrimSpace(string(out)); detail != "" {
			return i18n.NewError("无法退出应用：%s", detail)
		}
		return nil
	}
	return i18n.NewError("当前应用已切换")
}
