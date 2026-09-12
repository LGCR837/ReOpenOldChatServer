package verify

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math/rand"
	"time"
)

var digitPatterns = [10][7]string{
	{"01110", "10001", "10001", "10001", "10001", "10001", "01110"}, // 0
	{"00100", "01100", "00100", "00100", "00100", "00100", "01110"}, // 1
	{"01110", "10001", "00001", "00010", "00100", "01000", "11111"}, // 2
	{"11110", "00001", "00001", "01110", "00001", "00001", "11110"}, // 3
	{"00010", "00110", "01010", "10010", "11111", "00010", "00010"}, // 4
	{"11111", "10000", "11110", "00001", "00001", "10001", "01110"}, // 5
	{"00110", "01000", "10000", "11110", "10001", "10001", "01110"}, // 6
	{"11111", "00001", "00010", "00100", "01000", "01000", "01000"}, // 7
	{"01110", "10001", "10001", "01110", "10001", "10001", "01110"}, // 8
	{"01110", "10001", "10001", "01111", "00001", "00010", "01100"}, // 9
}

func RandomDigits(length int) string {
	if length <= 0 {
		length = 4
	}
	rand.Seed(time.Now().UnixNano())
	out := make([]byte, length)
	for i := 0; i < length; i++ {
		out[i] = byte('0' + rand.Intn(10))
	}
	return string(out)
}

func RenderCaptchaPNG(code string) ([]byte, error) {
	rand.Seed(time.Now().UnixNano())
	scale := 4
	padding := 6
	digitW := 5 * scale
	digitH := 7 * scale
	space := 4

	width := padding*2 + len(code)*digitW + (len(code)-1)*space
	height := padding*2 + digitH
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	bg := color.RGBA{245, 245, 245, 255}
	draw.Draw(img, img.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)

	for i, ch := range code {
		if ch < '0' || ch > '9' {
			continue
		}
		jx := rand.Intn(3) - 1
		jy := rand.Intn(3) - 1
		x0 := padding + i*(digitW+space) + jx
		y0 := padding + jy
		drawDigit(img, x0, y0, int(ch-'0'), scale)
	}

	for i := 0; i < 6; i++ {
		drawNoiseLine(img)
	}
	drawNoiseDots(img, 120)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func drawDigit(img *image.RGBA, x0, y0, digit, scale int) {
	pattern := digitPatterns[digit]
	col := color.RGBA{uint8(20 + rand.Intn(80)), uint8(20 + rand.Intn(80)), uint8(20 + rand.Intn(80)), 255}
	for y := 0; y < len(pattern); y++ {
		row := pattern[y]
		for x := 0; x < len(row); x++ {
			if row[x] != '1' {
				continue
			}
			x1 := x0 + x*scale
			y1 := y0 + y*scale
			rect := image.Rect(x1, y1, x1+scale, y1+scale)
			draw.Draw(img, rect, &image.Uniform{col}, image.Point{}, draw.Src)
		}
	}
}

func drawNoiseLine(img *image.RGBA) {
	b := img.Bounds()
	x1 := rand.Intn(b.Max.X)
	y1 := rand.Intn(b.Max.Y)
	x2 := rand.Intn(b.Max.X)
	y2 := rand.Intn(b.Max.Y)
	col := color.RGBA{uint8(120 + rand.Intn(80)), uint8(120 + rand.Intn(80)), uint8(120 + rand.Intn(80)), 255}
	drawLine(img, x1, y1, x2, y2, col)
}

func drawNoiseDots(img *image.RGBA, count int) {
	b := img.Bounds()
	for i := 0; i < count; i++ {
		x := rand.Intn(b.Max.X)
		y := rand.Intn(b.Max.Y)
		col := color.RGBA{uint8(80 + rand.Intn(120)), uint8(80 + rand.Intn(120)), uint8(80 + rand.Intn(120)), 255}
		img.Set(x, y, col)
	}
}

func drawLine(img *image.RGBA, x1, y1, x2, y2 int, c color.RGBA) {
	dx := abs(x2 - x1)
	dy := -abs(y2 - y1)
	sx := -1
	if x1 < x2 {
		sx = 1
	}
	sy := -1
	if y1 < y2 {
		sy = 1
	}
	err := dx + dy
	for {
		img.Set(x1, y1, c)
		if x1 == x2 && y1 == y2 {
			break
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x1 += sx
		}
		if e2 <= dx {
			err += dx
			y1 += sy
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
