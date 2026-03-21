"""Tests for Spectra 6 dithering pipeline."""

from PIL import Image

from petcast.dither import (
    SPECTRA6_PALETTE,
    _floyd_steinberg_dither,
    _resize_crop,
)


def _make_solid(color: tuple[int, int, int], w: int = 100, h: int = 100) -> Image.Image:
    return Image.new("RGB", (w, h), color)


def _dominant_color(img: Image.Image) -> tuple[int, int, int]:
    colors = img.getcolors(maxcolors=img.width * img.height)
    colors.sort(key=lambda x: -x[0])
    return colors[0][1]


def _color_set(img: Image.Image) -> set[tuple[int, int, int]]:
    colors = img.getcolors(maxcolors=img.width * img.height)
    return {c for _, c in colors}


class TestOutputPalette:
    """Dithered output must ONLY contain palette colors."""

    def test_solid_red_outputs_palette_colors(self):
        img = _make_solid((200, 30, 30))
        result = _floyd_steinberg_dither(img, SPECTRA6_PALETTE)
        colors = _color_set(result)
        assert colors <= set(SPECTRA6_PALETTE), f"Non-palette colors: {colors - set(SPECTRA6_PALETTE)}"

    def test_solid_white(self):
        img = _make_solid((255, 255, 255))
        result = _floyd_steinberg_dither(img, SPECTRA6_PALETTE)
        assert _dominant_color(result) == (255, 255, 255)

    def test_solid_black(self):
        img = _make_solid((0, 0, 0))
        result = _floyd_steinberg_dither(img, SPECTRA6_PALETTE)
        assert _dominant_color(result) == (0, 0, 0)

    def test_gradient_only_palette_colors(self):
        img = Image.new("RGB", (100, 100))
        for y in range(100):
            for x in range(100):
                img.putpixel((x, y), (int(x * 2.55), int(y * 2.55), 128))
        result = _floyd_steinberg_dither(img, SPECTRA6_PALETTE)
        colors = _color_set(result)
        assert colors <= set(SPECTRA6_PALETTE), f"Non-palette colors: {colors - set(SPECTRA6_PALETTE)}"


class TestPerceptualColorMatching:
    """Colors should map to perceptually correct palette entries."""

    def test_dark_gray_does_not_map_to_green(self):
        img = _make_solid((70, 80, 90))
        result = _floyd_steinberg_dither(img, SPECTRA6_PALETTE)
        dominant = _dominant_color(result)
        assert dominant != (0, 150, 0), f"Dark gray mapped to green"

    def test_dark_blue_gray_maps_to_black_or_blue(self):
        img = _make_solid((60, 70, 110))
        result = _floyd_steinberg_dither(img, SPECTRA6_PALETTE)
        dominant = _dominant_color(result)
        assert dominant in ((0, 0, 0), (0, 0, 200)), f"Dark blue-gray mapped to {dominant}"

    def test_medium_gray_has_black(self):
        """Medium gray should include black pixels, not just chromatic colors."""
        img = _make_solid((128, 128, 128))
        result = _floyd_steinberg_dither(img, SPECTRA6_PALETTE)
        colors = _color_set(result)
        assert (0, 0, 0) in colors or (255, 255, 255) in colors, \
            f"Medium gray should have black or white, got: {colors}"

    def test_orange_has_red_or_yellow(self):
        img = _make_solid((200, 120, 40))
        result = _floyd_steinberg_dither(img, SPECTRA6_PALETTE)
        colors = _color_set(result)
        assert (200, 0, 0) in colors or (255, 230, 0) in colors, \
            f"Orange should have red or yellow, got: {colors}"

    def test_pure_white_stays_white(self):
        assert _dominant_color(_floyd_steinberg_dither(_make_solid((255, 255, 255)), SPECTRA6_PALETTE)) == (255, 255, 255)

    def test_pure_black_stays_black(self):
        assert _dominant_color(_floyd_steinberg_dither(_make_solid((0, 0, 0)), SPECTRA6_PALETTE)) == (0, 0, 0)


class TestResizeCrop:
    def test_landscape_to_landscape(self):
        result = _resize_crop(Image.new("RGB", (1536, 1024)), 800, 480)
        assert result.size == (800, 480)

    def test_square_to_landscape(self):
        result = _resize_crop(Image.new("RGB", (1024, 1024)), 800, 480)
        assert result.size == (800, 480)

    def test_small_upscales(self):
        result = _resize_crop(Image.new("RGB", (400, 240)), 800, 480)
        assert result.size == (800, 480)
