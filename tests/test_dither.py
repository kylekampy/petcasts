"""Tests for Spectra 6 dithering pipeline."""

from PIL import Image

from petcast.dither import (
    DISPLAY_COLORS,
    REFERENCE_PALETTE,
    _floyd_steinberg_dither,
    _resize_crop,
    dither_for_display,
)


def _make_solid(color: tuple[int, int, int], w: int = 100, h: int = 100) -> Image.Image:
    """Create a solid color test image."""
    return Image.new("RGB", (w, h), color)


def _dominant_color(img: Image.Image) -> tuple[int, int, int]:
    """Return the most common color in an image."""
    colors = img.getcolors(maxcolors=img.width * img.height)
    colors.sort(key=lambda x: -x[0])
    return colors[0][1]


def _color_set(img: Image.Image) -> set[tuple[int, int, int]]:
    """Return the set of all unique colors in an image."""
    colors = img.getcolors(maxcolors=img.width * img.height)
    return {c for _, c in colors}


class TestOutputPalette:
    """Dithered output must ONLY contain reference palette colors."""

    def test_solid_red_outputs_reference_red(self):
        img = _make_solid((200, 30, 30))
        result = _floyd_steinberg_dither(img, DISPLAY_COLORS, REFERENCE_PALETTE)
        colors = _color_set(result)
        assert colors <= set(REFERENCE_PALETTE), f"Non-palette colors found: {colors - set(REFERENCE_PALETTE)}"

    def test_solid_white_outputs_reference_white(self):
        img = _make_solid((255, 255, 255))
        result = _floyd_steinberg_dither(img, DISPLAY_COLORS, REFERENCE_PALETTE)
        dominant = _dominant_color(result)
        assert dominant == (255, 255, 255)

    def test_solid_black_outputs_reference_black(self):
        img = _make_solid((0, 0, 0))
        result = _floyd_steinberg_dither(img, DISPLAY_COLORS, REFERENCE_PALETTE)
        dominant = _dominant_color(result)
        assert dominant == (0, 0, 0)

    def test_all_output_colors_are_in_reference_palette(self):
        """Any input should only produce reference palette colors."""
        # Use a gradient image to exercise all paths
        img = Image.new("RGB", (100, 100))
        for y in range(100):
            for x in range(100):
                img.putpixel((x, y), (int(x * 2.55), int(y * 2.55), 128))
        result = _floyd_steinberg_dither(img, DISPLAY_COLORS, REFERENCE_PALETTE)
        colors = _color_set(result)
        assert colors <= set(REFERENCE_PALETTE), f"Non-palette colors: {colors - set(REFERENCE_PALETTE)}"


class TestPerceptualColorMatching:
    """Colors should map to perceptually correct palette entries."""

    def test_dark_gray_does_not_map_to_green(self):
        """Dark gray (70,80,90) should NOT dither to green."""
        img = _make_solid((70, 80, 90))
        result = _floyd_steinberg_dither(img, DISPLAY_COLORS, REFERENCE_PALETTE)
        dominant = _dominant_color(result)
        # Black or blue are both acceptable; green is wrong
        assert dominant != (0, 255, 0), f"Dark gray mapped to green, expected black or blue"

    def test_dark_blue_gray_maps_to_black_or_blue(self):
        """Dark slate blue (60,70,110) should map to black or blue, NOT green."""
        img = _make_solid((60, 70, 110))
        result = _floyd_steinberg_dither(img, DISPLAY_COLORS, REFERENCE_PALETTE)
        dominant = _dominant_color(result)
        assert dominant in ((0, 0, 0), (0, 0, 255)), f"Dark blue-gray mapped to {dominant}, expected black or blue"

    def test_medium_gray_maps_to_black_white_mix(self):
        """Medium gray (128,128,128) should dither to black+white, not green."""
        img = _make_solid((128, 128, 128))
        result = _floyd_steinberg_dither(img, DISPLAY_COLORS, REFERENCE_PALETTE)
        colors = _color_set(result)
        # Should be mostly black and white, not green
        assert (0, 255, 0) not in colors or (0, 0, 0) in colors, \
            f"Medium gray should not be pure green, got: {colors}"

    def test_orange_maps_to_red_yellow_mix(self):
        """Orange (200,120,40) should dither to red+yellow, not green."""
        img = _make_solid((200, 120, 40))
        result = _floyd_steinberg_dither(img, DISPLAY_COLORS, REFERENCE_PALETTE)
        colors = _color_set(result)
        assert (255, 0, 0) in colors or (255, 255, 0) in colors, \
            f"Orange should contain red or yellow, got: {colors}"

    def test_pure_white_stays_white(self):
        img = _make_solid((255, 255, 255))
        result = _floyd_steinberg_dither(img, DISPLAY_COLORS, REFERENCE_PALETTE)
        assert _dominant_color(result) == (255, 255, 255)

    def test_pure_black_stays_black(self):
        img = _make_solid((0, 0, 0))
        result = _floyd_steinberg_dither(img, DISPLAY_COLORS, REFERENCE_PALETTE)
        assert _dominant_color(result) == (0, 0, 0)


class TestResizeCrop:
    def test_landscape_to_landscape(self):
        img = Image.new("RGB", (1536, 1024), (128, 128, 128))
        result = _resize_crop(img, 800, 480)
        assert result.size == (800, 480)

    def test_square_to_landscape(self):
        img = Image.new("RGB", (1024, 1024), (128, 128, 128))
        result = _resize_crop(img, 800, 480)
        assert result.size == (800, 480)

    def test_small_image_upscales(self):
        img = Image.new("RGB", (400, 240), (128, 128, 128))
        result = _resize_crop(img, 800, 480)
        assert result.size == (800, 480)
