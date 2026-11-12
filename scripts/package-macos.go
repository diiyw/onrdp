//go:build ignore

// Invoked explicitly by build-macos.sh so Onix installs its resource overlay.
package main

import (
	"fmt"
	"github.com/onix-engine/onix/editor"
	"log"
	"os"
	"path/filepath"
	"runtime"
)

func main() {
	root, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		log.Fatal(err)
	}
	options := editor.DefaultBuildOptions(root)
	options.AppName = "OnRDP"
	options.Platform = editor.BuildPlatformMacOS
	options.Mode = editor.BuildModeRelease
	options.PackageFormat = editor.BuildPackageApp
	arch := os.Getenv("GOARCH")
	if arch == "" {
		arch = runtime.GOARCH
	}
	options.Architecture = editor.BuildArchitecture(arch)
	options.Progress = func(progress editor.BuildProgress) { fmt.Println(progress.Message) }
	artifact, err := editor.BuildProject(options)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(artifact)
}
