package main

import (
	"log"
	"os"

	"gioui.org/app"

	"github.com/hussaratkuro/hyprdb/internal/ui"
)

func main() {
	go func() {
		if err := ui.Run(); err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}()
	app.Main()
}
