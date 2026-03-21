"""Spectra 6 e-ink dithering pipeline.

Matches the ESPHome epaper_spi Spectra E6 driver's color classification:
grayscale check (spread < 50), then binary threshold at 128 per channel.
"""

import numpy as np
from PIL import Image, ImageEnhance

from petcast.config import Config

# Output palette values — one per driver bucket
# These must pass through the driver's classification correctly
SPECTRA6_PALETTE = {
    "BLACK": (0, 0, 0),
    "WHITE": (255, 255, 255),
    "RED": (200, 0, 0),
    "GREEN": (0, 150, 0),
    "BLUE": (0, 0, 200),
    "YELLOW": (255, 230, 0),
}


def _driver_classify(r: float, g: float, b: float) -> str:
    """Replicate the ESPHome Spectra E6 driver's color classification."""
    ri, gi, bi = int(np.clip(r, 0, 255)), int(np.clip(g, 0, 255)), int(np.clip(b, 0, 255))
    spread = max(ri, gi, bi) - min(ri, gi, bi)

    if spread < 50:
        # Grayscale
        return "WHITE" if (ri + gi + bi) > 382 else "BLACK"

    # Binary threshold per channel
    ro = ri > 128
    go = gi > 128
    bo = bi > 128

    if ro and go and not bo:
        return "YELLOW"
    if ro and not go and not bo:
        return "RED"
    if not ro and go and not bo:
        return "GREEN"
    if not ro and not go and bo:
        return "BLUE"
    if not ro and go and bo:
        return "GREEN"  # cyan → green
    if ro and not go and bo:
        return "RED"    # magenta → red
    if ro and go and bo:
        return "WHITE"
    return "BLACK"


def dither_for_display(image: Image.Image, config: Config) -> Image.Image:
    """Resize, enhance, and dither an image for Spectra 6 e-ink display."""
    w, h = config.display.width, config.display.height

    img = image.convert("RGB")
    img = _resize_crop(img, w, h)

    # Boost contrast and saturation for e-ink
    img = ImageEnhance.Contrast(img).enhance(1.2)
    img = ImageEnhance.Color(img).enhance(1.3)

    img = _floyd_steinberg_dither(img)

    return img


def _resize_crop(img: Image.Image, target_w: int, target_h: int) -> Image.Image:
    """Resize and center-crop to exactly target dimensions."""
    src_w, src_h = img.size
    target_ratio = target_w / target_h
    src_ratio = src_w / src_h

    if src_ratio > target_ratio:
        new_h = target_h
        new_w = int(src_w * (target_h / src_h))
    else:
        new_w = target_w
        new_h = int(src_h * (target_w / src_w))

    img = img.resize((new_w, new_h), Image.Resampling.LANCZOS)

    left = (new_w - target_w) // 2
    top = (new_h - target_h) // 2
    return img.crop((left, top, left + target_w, top + target_h))


def _floyd_steinberg_dither(img: Image.Image) -> Image.Image:
    """Floyd-Steinberg dithering using the driver's own color classification."""
    pixels = np.array(img, dtype=np.float64)
    h, w, _ = pixels.shape
    palette = SPECTRA6_PALETTE

    for y in range(h):
        for x in range(w):
            old = pixels[y, x].copy()

            # Classify using the same logic as the display driver
            color_name = _driver_classify(old[0], old[1], old[2])
            new = np.array(palette[color_name], dtype=np.float64)

            pixels[y, x] = new
            error = old - new

            # Distribute error to neighbors
            if x + 1 < w:
                pixels[y, x + 1] += error * (7 / 16)
            if y + 1 < h:
                if x - 1 >= 0:
                    pixels[y + 1, x - 1] += error * (3 / 16)
                pixels[y + 1, x] += error * (5 / 16)
                if x + 1 < w:
                    pixels[y + 1, x + 1] += error * (1 / 16)

    pixels = np.clip(pixels, 0, 255).astype(np.uint8)
    return Image.fromarray(pixels)
