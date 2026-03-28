package pipeline

import (
	"image"
	"image/color"
	"math"

	"golang.org/x/image/draw"
)

// Spectra6Palette is the 6-color palette for Spectra 6 e-ink displays.
var Spectra6Palette = []color.NRGBA{
	{0, 0, 0, 255},       // Black
	{255, 255, 255, 255},  // White
	{200, 0, 0, 255},      // Red
	{0, 150, 0, 255},      // Green
	{0, 0, 200, 255},      // Blue
	{255, 230, 0, 255},    // Yellow
}

// DitherForDisplay resizes, enhances, and applies Atkinson dithering for e-ink.
func DitherForDisplay(src image.Image, targetW, targetH int) *image.NRGBA {
	// Resize and center-crop
	resized := resizeCrop(src, targetW, targetH)

	// Boost contrast (1.2) and saturation (1.3) for e-ink
	enhanced := enhanceContrast(resized, 1.2)
	enhanced = enhanceSaturation(enhanced, 1.3)

	// Atkinson dithering
	return atkinsonDither(enhanced, Spectra6Palette)
}

func resizeCrop(src image.Image, targetW, targetH int) *image.NRGBA {
	srcBounds := src.Bounds()
	srcW := srcBounds.Dx()
	srcH := srcBounds.Dy()

	targetRatio := float64(targetW) / float64(targetH)
	srcRatio := float64(srcW) / float64(srcH)

	var newW, newH int
	if srcRatio > targetRatio {
		newH = targetH
		newW = int(float64(srcW) * (float64(targetH) / float64(srcH)))
	} else {
		newW = targetW
		newH = int(float64(srcH) * (float64(targetW) / float64(srcW)))
	}

	// Resize with CatmullRom (high quality)
	scaled := image.NewNRGBA(image.Rect(0, 0, newW, newH))
	draw.CatmullRom.Scale(scaled, scaled.Bounds(), src, srcBounds, draw.Over, nil)

	// Center crop
	left := (newW - targetW) / 2
	top := (newH - targetH) / 2
	cropped := image.NewNRGBA(image.Rect(0, 0, targetW, targetH))
	draw.Copy(cropped, image.Point{}, scaled, image.Rect(left, top, left+targetW, top+targetH), draw.Src, nil)
	return cropped
}

func enhanceContrast(img *image.NRGBA, factor float64) *image.NRGBA {
	bounds := img.Bounds()
	out := image.NewNRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := img.NRGBAAt(x, y)
			r := clampU8(128 + factor*(float64(c.R)-128))
			g := clampU8(128 + factor*(float64(c.G)-128))
			b := clampU8(128 + factor*(float64(c.B)-128))
			out.SetNRGBA(x, y, color.NRGBA{r, g, b, c.A})
		}
	}
	return out
}

func enhanceSaturation(img *image.NRGBA, factor float64) *image.NRGBA {
	bounds := img.Bounds()
	out := image.NewNRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := img.NRGBAAt(x, y)
			gray := 0.2989*float64(c.R) + 0.5870*float64(c.G) + 0.1140*float64(c.B)
			r := clampU8(gray + factor*(float64(c.R)-gray))
			g := clampU8(gray + factor*(float64(c.G)-gray))
			b := clampU8(gray + factor*(float64(c.B)-gray))
			out.SetNRGBA(x, y, color.NRGBA{r, g, b, c.A})
		}
	}
	return out
}

// atkinsonDither applies Atkinson dithering with the given palette.
// Diffuses 1/8 of error to 6 neighbors (75% total error diffusion).
func atkinsonDither(img *image.NRGBA, palette []color.NRGBA) *image.NRGBA {
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()

	// Work with float64 pixel buffer for error diffusion
	pixels := make([]float64, w*h*3)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := img.NRGBAAt(x+bounds.Min.X, y+bounds.Min.Y)
			idx := (y*w + x) * 3
			pixels[idx] = float64(c.R)
			pixels[idx+1] = float64(c.G)
			pixels[idx+2] = float64(c.B)
		}
	}

	// Pre-convert palette to float64
	pal := make([][3]float64, len(palette))
	for i, c := range palette {
		pal[i] = [3]float64{float64(c.R), float64(c.G), float64(c.B)}
	}

	out := image.NewNRGBA(image.Rect(0, 0, w, h))

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			idx := (y*w + x) * 3
			oldR := pixels[idx]
			oldG := pixels[idx+1]
			oldB := pixels[idx+2]

			// Find nearest palette color
			best := 0
			bestDist := math.MaxFloat64
			for i, p := range pal {
				dr := oldR - p[0]
				dg := oldG - p[1]
				db := oldB - p[2]
				dist := dr*dr + dg*dg + db*db
				if dist < bestDist {
					bestDist = dist
					best = i
				}
			}

			newR := pal[best][0]
			newG := pal[best][1]
			newB := pal[best][2]

			out.SetNRGBA(x, y, palette[best])

			// Error = (old - new) / 8
			errR := (oldR - newR) / 8.0
			errG := (oldG - newG) / 8.0
			errB := (oldB - newB) / 8.0

			// Diffuse to 6 neighbors (Atkinson pattern)
			diffuse := func(dx, dy int) {
				nx, ny := x+dx, y+dy
				if nx >= 0 && nx < w && ny >= 0 && ny < h {
					nIdx := (ny*w + nx) * 3
					pixels[nIdx] += errR
					pixels[nIdx+1] += errG
					pixels[nIdx+2] += errB
				}
			}
			diffuse(1, 0)
			diffuse(2, 0)
			diffuse(-1, 1)
			diffuse(0, 1)
			diffuse(1, 1)
			diffuse(0, 2)
		}
	}

	return out
}

func clampU8(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v + 0.5)
}
