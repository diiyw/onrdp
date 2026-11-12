//go:build !android && !ios

package main

import "github.com/onix-engine/onix"

func main() {
	if err := start(); err != nil {
		panic(err)
	}
}

func startDesktop(app *onix.App) error {
	app.SetInitialWindowSize(1024, 700)
	return nil
}
