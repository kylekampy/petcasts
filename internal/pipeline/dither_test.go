package pipeline

import (
	"image"
	"image/color"
	"testing"
)

func TestDitherForDisplay_OutputSize(t *testing.T) {
	// Create a test image larger than target
	src := image.NewNRGBA(image.Rect(0, 0, 1200, 800))
	for y := range 800 {
		for x := range 1200 {
			src.SetNRGBA(x, y, color.NRGBA{uint8(x % 256), uint8(y % 256), 128, 255})
		}
	}

	result := DitherForDisplay(src, 800, 480)

	bounds := result.Bounds()
	if bounds.Dx() != 800 || bounds.Dy() != 480 {
		t.Errorf("output size = %dx%d, want 800x480", bounds.Dx(), bounds.Dy())
	}
}

func TestDitherForDisplay_OnlyPaletteColors(t *testing.T) {
	// Create a simple gradient image
	src := image.NewNRGBA(image.Rect(0, 0, 80, 48))
	for y := range 48 {
		for x := range 80 {
			gray := uint8((x * 255) / 80)
			src.SetNRGBA(x, y, color.NRGBA{gray, gray, gray, 255})
		}
	}

	result := DitherForDisplay(src, 80, 48)

	// Every pixel must be a palette color
	paletteSet := make(map[color.NRGBA]bool)
	for _, c := range Spectra6Palette {
		paletteSet[c] = true
	}

	bounds := result.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := result.NRGBAAt(x, y)
			if !paletteSet[c] {
				t.Fatalf("pixel (%d,%d) = %v is not in Spectra 6 palette", x, y, c)
			}
		}
	}
}

func TestDitherForDisplay_SmallImage(t *testing.T) {
	// Smaller than target should still work (scale up + crop)
	src := image.NewNRGBA(image.Rect(0, 0, 100, 60))
	for y := range 60 {
		for x := range 100 {
			src.SetNRGBA(x, y, color.NRGBA{200, 0, 0, 255}) // solid red
		}
	}

	result := DitherForDisplay(src, 800, 480)
	bounds := result.Bounds()
	if bounds.Dx() != 800 || bounds.Dy() != 480 {
		t.Errorf("output size = %dx%d, want 800x480", bounds.Dx(), bounds.Dy())
	}

	// Most pixels should be the red palette color
	redCount := 0
	total := 800 * 480
	for y := range 480 {
		for x := range 800 {
			c := result.NRGBAAt(x, y)
			if c.R == 200 && c.G == 0 && c.B == 0 {
				redCount++
			}
		}
	}
	ratio := float64(redCount) / float64(total)
	if ratio < 0.5 {
		t.Errorf("solid red input produced only %.1f%% red pixels, expected > 50%%", ratio*100)
	}
}

func TestResizeCrop(t *testing.T) {
	tests := []struct {
		name         string
		srcW, srcH   int
		tgtW, tgtH   int
		wantW, wantH int
	}{
		{"wider source", 1600, 800, 800, 480, 800, 480},
		{"taller source", 600, 1200, 800, 480, 800, 480},
		{"exact size", 800, 480, 800, 480, 800, 480},
		{"smaller source", 400, 240, 800, 480, 800, 480},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := image.NewNRGBA(image.Rect(0, 0, tt.srcW, tt.srcH))
			result := resizeCrop(src, tt.tgtW, tt.tgtH)
			bounds := result.Bounds()
			if bounds.Dx() != tt.wantW || bounds.Dy() != tt.wantH {
				t.Errorf("resizeCrop(%dx%d → %dx%d) = %dx%d, want %dx%d",
					tt.srcW, tt.srcH, tt.tgtW, tt.tgtH,
					bounds.Dx(), bounds.Dy(), tt.wantW, tt.wantH)
			}
		})
	}
}

func TestEnhanceContrast(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.SetNRGBA(0, 0, color.NRGBA{100, 100, 100, 255})
	img.SetNRGBA(1, 0, color.NRGBA{200, 200, 200, 255})

	result := enhanceContrast(img, 1.5)

	// Pixel below 128 should get darker, pixel above 128 should get brighter
	c0 := result.NRGBAAt(0, 0)
	c1 := result.NRGBAAt(1, 0)

	if c0.R >= 100 {
		t.Errorf("contrast enhanced dark pixel R=%d, expected < 100", c0.R)
	}
	if c1.R <= 200 {
		t.Errorf("contrast enhanced bright pixel R=%d, expected > 200", c1.R)
	}
}

func TestEnhanceSaturation(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.SetNRGBA(0, 0, color.NRGBA{200, 100, 100, 255})

	result := enhanceSaturation(img, 1.5)
	c := result.NRGBAAt(0, 0)

	// Red channel should be boosted further from gray
	if c.R <= 200 {
		t.Errorf("saturation enhanced R=%d, expected > 200", c.R)
	}
}

func TestClampU8(t *testing.T) {
	if v := clampU8(-10); v != 0 {
		t.Errorf("clampU8(-10) = %d, want 0", v)
	}
	if v := clampU8(300); v != 255 {
		t.Errorf("clampU8(300) = %d, want 255", v)
	}
	if v := clampU8(127.6); v != 128 {
		t.Errorf("clampU8(127.6) = %d, want 128", v)
	}
}
