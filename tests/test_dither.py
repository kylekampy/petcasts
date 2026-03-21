"""Tests for Spectra 6 dithering pipeline."""

from PIL import Image

from petcast.dither import (
    SPECTRA6_PALETTE,
    _driver_classify,
    _floyd_steinberg_dither,
    _resize_crop,
)

PALETTE_COLORS = set(SPECTRA6_PALETTE.values())


def _make_solid(color: tuple[int, int, int], w: int = 100, h: int = 100) -> Image.Image:
    return Image.new("RGB", (w, h), color)


def _dominant_color(img: Image.Image) -> tuple[int, int, int]:
    colors = img.getcolors(maxcolors=img.width * img.height)
    colors.sort(key=lambda x: -x[0])
    return colors[0][1]


def _color_set(img: Image.Image) -> set[tuple[int, int, int]]:
    colors = img.getcolors(maxcolors=img.width * img.height)
    return {c for _, c in colors}


class TestDriverClassify:
    """Test that _driver_classify matches the ESPHome Spectra E6 driver."""

    def test_pure_red(self):
        assert _driver_classify(200, 0, 0) == "RED"

    def test_pure_green(self):
        assert _driver_classify(0, 200, 0) == "GREEN"

    def test_pure_blue(self):
        assert _driver_classify(0, 0, 200) == "BLUE"

    def test_pure_yellow(self):
        assert _driver_classify(200, 200, 0) == "YELLOW"

    def test_pure_black(self):
        assert _driver_classify(0, 0, 0) == "BLACK"

    def test_pure_white(self):
        assert _driver_classify(255, 255, 255) == "WHITE"

    def test_dark_gray_is_black(self):
        assert _driver_classify(70, 80, 90) == "BLACK"

    def test_light_gray_is_white(self):
        assert _driver_classify(180, 180, 180) == "WHITE"

    def test_medium_gray_is_black(self):
        # (128+128+128) = 384 > 382, but spread < 50 → grayscale → WHITE
        assert _driver_classify(128, 128, 128) == "WHITE"

    def test_dark_blue_gray_is_blue(self):
        # spread = 160-60 = 100 >= 50, B>128, R<128, G<128
        assert _driver_classify(60, 70, 160) == "BLUE"

    def test_cyan_maps_to_green(self):
        assert _driver_classify(0, 200, 200) == "GREEN"

    def test_magenta_maps_to_red(self):
        assert _driver_classify(200, 0, 200) == "RED"

    def test_grayscale_threshold(self):
        # spread = 49 < 50 → grayscale
        assert _driver_classify(100, 100, 149) == "BLACK"
        # spread = 50 → NOT grayscale, B>128
        assert _driver_classify(100, 100, 150) == "BLUE"


class TestDitheringOutput:
    """Dithered output must ONLY contain palette colors."""

    def test_solid_colors_output_palette(self):
        for color in [(200, 30, 30), (30, 200, 30), (30, 30, 200), (200, 200, 30)]:
            result = _floyd_steinberg_dither(_make_solid(color))
            colors = _color_set(result)
            assert colors <= PALETTE_COLORS, f"{color} produced non-palette: {colors - PALETTE_COLORS}"

    def test_gradient_only_palette_colors(self):
        img = Image.new("RGB", (100, 100))
        for y in range(100):
            for x in range(100):
                img.putpixel((x, y), (int(x * 2.55), int(y * 2.55), 128))
        result = _floyd_steinberg_dither(img)
        colors = _color_set(result)
        assert colors <= PALETTE_COLORS

    def test_pure_white_stays_white(self):
        assert _dominant_color(_floyd_steinberg_dither(_make_solid((255, 255, 255)))) == (255, 255, 255)

    def test_pure_black_stays_black(self):
        assert _dominant_color(_floyd_steinberg_dither(_make_solid((0, 0, 0)))) == (0, 0, 0)

    def test_blue_image_has_blue_pixels(self):
        """A distinctly blue image should produce blue pixels."""
        result = _floyd_steinberg_dither(_make_solid((50, 70, 180)))
        colors = _color_set(result)
        assert SPECTRA6_PALETTE["BLUE"] in colors, f"Blue image has no blue pixels: {colors}"


class TestResizeCrop:
    def test_landscape_to_landscape(self):
        assert _resize_crop(Image.new("RGB", (1536, 1024)), 800, 480).size == (800, 480)

    def test_square_to_landscape(self):
        assert _resize_crop(Image.new("RGB", (1024, 1024)), 800, 480).size == (800, 480)

    def test_small_upscales(self):
        assert _resize_crop(Image.new("RGB", (400, 240)), 800, 480).size == (800, 480)
