package main

import (
	"image"
	"image/color"
	"image/jpeg"
	"os"
)

// 720x1280 portrait test image: red top half, blue bottom half
func main() {
	img := image.NewRGBA(image.Rect(0, 0, 720, 1280))
	for y := 0; y < 1280; y++ {
		for x := 0; x < 720; x++ {
			c := color.RGBA{220, 30, 30, 255}
			if y >= 640 {
				c = color.RGBA{30, 30, 220, 255}
			}
			img.Set(x, y, c)
		}
	}
	f, _ := os.Create(os.Args[1])
	jpeg.Encode(f, img, &jpeg.Options{Quality: 85})
	f.Close()
}
