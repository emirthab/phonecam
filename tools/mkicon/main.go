// mkicon generates assets/phonecam.ico and docs/logo.png.
//
//	go run ./tools/mkicon
package main

import (
	"image/png"
	"log"
	"os"

	"github.com/emirthab/phonecam/internal/icon"
)

func main() {
	os.MkdirAll("assets", 0o755)
	os.MkdirAll("docs", 0o755)
	ico := icon.ICO([]int{16, 20, 24, 32, 40, 48, 64, 128, 256})
	if err := os.WriteFile("assets/phonecam.ico", ico, 0o644); err != nil {
		log.Fatal(err)
	}
	f, err := os.Create("docs/logo.png")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, icon.Render(256, icon.StatusNone)); err != nil {
		log.Fatal(err)
	}
}
