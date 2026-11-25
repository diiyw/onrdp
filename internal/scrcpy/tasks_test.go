package scrcpy

import "testing"

func TestMuMuTaskMoveRequiresUnambiguousNewScreen(t *testing.T) {
	home := Display{ID: 0, Home: true}
	mirror := Display{ID: 13, capture: true, TaskID: 81, App: "example"}
	target := Display{ID: 14, mumuScreen: true, TasksKnown: true}
	occupied := target
	occupied.hasRoot = true // Even an invisible task owns its display.
	for _, tc := range []struct {
		name        string
		before, now []Display
		move        bool
	}{
		{"misrouted launch", []Display{home}, []Display{home, mirror, target}, true},
		{"existing display", []Display{home, target}, []Display{home, mirror, target}, false},
		{"occupied", []Display{home}, []Display{home, mirror, occupied}, false},
		{"two mirrors", []Display{home}, []Display{home, mirror, target, {ID: 15, capture: true}}, false},
		{"two new screens", []Display{home}, []Display{home, mirror, target, {ID: 15, mumuScreen: true}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			move, ok := misplacedMuMuTask(tc.now, tc.before)
			if ok != tc.move || (ok && (move.task != 81 || move.destination != 14 || move.source != 13)) {
				t.Fatalf("move %+v, ok %t", move, ok)
			}
		})
	}
}

func TestRemoveOnlyIndependentForegroundTask(t *testing.T) {
	const listing = `RootTask id=1 displayId=0
 configuration={mActivityType=home info}
 taskId=2: launcher visible=true topActivity=ComponentInfo{launcher/Home}
RootTask id=8 displayId=0
 configuration={mActivityType=standard info}
 taskId=8: background visible=false topActivity=ComponentInfo{background/Main}
RootTask id=18 displayId=15
 configuration={mActivityType=standard info}
 taskId=18: game visible=true topActivity=ComponentInfo{game/Main}
RootTask id=19 displayId=22
 configuration={mActivityType=standard info}
 taskId=20: other visible=false
 taskId=21: app visible=true topActivity=ComponentInfo{app/Main}
`
	displays := []Display{{ID: 0}, {ID: 15}, {ID: 22}}
	applyTasks(displays, listing)
	if displays[0].TaskID != 0 || displays[0].App != "" || displays[1].TaskID != 18 || displays[1].App != "game" || displays[2].TaskID != 0 || displays[2].App != "app" {
		t.Fatalf("unsafe or missing task mapping: %+v", displays)
	}
	for _, d := range displays {
		if !d.TasksKnown {
			t.Fatal("task snapshot not recognized")
		}
	}
}
