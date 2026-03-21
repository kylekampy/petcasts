"""Tests for Spectra 6 dithering pipeline."""

from PIL import Image

from petcast.dither import SPECTRA6_PALETTE, _floyd_steinberg_dither, _resize_crop

PALETTE_SET = set(SPECTRA6_PALETTE)


def _make_solid(color, w=100, h=100):
    return Image.new("RGB", (w, h), color)


def _dominant_color(img):
    colors = img.getcolors(maxcolors=img.width * img.height)
    colors.sort(key=lambda x: -x[0])
    return colors[0][1]


def _color_set(img):
    colors = img.getcolors(maxcolors=img.width * img.height)
    return {c for _, c in colors}


class TestDitheringOutput:
    def test_only_palette_colors(self):
        img = Image.new("RGB", (100, 100))
        for y in range(100):
            for x in range(100):
                img.putpixel((x, y), (int(x * 2.55), int(y * 2.55), 128))
        result = _floyd_steinberg_dither(img, SPECTRA6_PALETTE)
        assert _color_set(result) <= PALETTE_SET

    def test_pure_white(self):
        assert _dominant_color(_floyd_steinberg_dither(_make_solid((255, 255, 255)), SPECTRA6_PALETTE)) == (255, 255, 255)

    def test_pure_black(self):
        assert _dominant_color(_floyd_steinberg_dither(_make_solid((0, 0, 0)), SPECTRA6_PALETTE)) == (0, 0, 0)

    def test_red_image_has_red(self):
        result = _floyd_steinberg_dither(_make_solid((200, 30, 30)), SPECTRA6_PALETTE)
        assert (200, 0, 0) in _color_set(result)

    def test_green_image_has_green(self):
        result = _floyd_steinberg_dither(_make_solid((30, 180, 30)), SPECTRA6_PALETTE)
        assert (0, 150, 0) in _color_set(result)

    def test_blue_image_has_blue(self):
        result = _floyd_steinberg_dither(_make_solid((30, 30, 200)), SPECTRA6_PALETTE)
        assert (0, 0, 200) in _color_set(result)


class TestResizeCrop:
    def test_landscape(self):
        assert _resize_crop(Image.new("RGB", (1536, 1024)), 800, 480).size == (800, 480)

    def test_square(self):
        assert _resize_crop(Image.new("RGB", (1024, 1024)), 800, 480).size == (800, 480)

    def test_upscale(self):
        assert _resize_crop(Image.new("RGB", (400, 240)), 800, 480).size == (800, 480)
