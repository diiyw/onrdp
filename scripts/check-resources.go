//go:build ignore

// Check the packaged filesystem, including icons selected dynamically by Go.
// Run from the project root: go run ./scripts/check-resources.go <resource-dir>
package main

import (
	"fmt"
	"io/fs"
	"log"
	"os"

	"github.com/onix-engine/onix/graphics"
	graphicssvg "github.com/onix-engine/onix/graphics/codec/svg"
	"github.com/onix-engine/onix/resource"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: check-resources <packaged-resource-directory>")
	}
	manager, err := resource.OpenManager(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	defer manager.Close()
	if err := manager.LoadRequired(); err != nil {
		log.Fatal(err)
	}
	icons, err := fs.Glob(os.DirFS("."), "resources/app/icons/*.svg")
	if err != nil || len(icons) == 0 {
		log.Fatal("run from the project root containing resources/app/icons")
	}
	decoder := graphicssvg.Decoder{}
	for _, path := range icons {
		data, err := fs.ReadFile(manager, path)
		if err != nil {
			log.Fatalf("packaged icon %s: %v", path, err)
		}
		texture, err := decoder.DecodeImage(data, graphics.TextureOptions{})
		if err != nil {
			log.Fatalf("decode packaged icon %s: %v", path, err)
		}
		if texture == nil || !texture.Valid() {
			log.Fatalf("packaged icon %s has no valid texture", path)
		}
		texture.Dispose()
	}
	fmt.Printf("Verified %d packaged SVG icons\n", len(icons))
}
