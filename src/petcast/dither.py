"""Spectra 6 e-ink dithering pipeline.

Uses Atkinson dithering which diffuses only 75% of quantization error,
producing cleaner edges and less color fringing than Floyd-Steinberg.
"""

import numpy as np
from PIL import Image, ImageEnhance

from petcast.config import Config

# Spectra 6 palette (sRGB values)
SPECTRA6_PALETTE = [
    (0, 0, 0),        # Black
    (255, 255, 255),   # White
    (200, 0, 0),       # Red
    (0, 150, 0),       # Green
    (0, 0, 200),       # Blue
    (255, 230, 0),     # Yellow
]


def dither_for_display(image: Image.Image, config: Config) -> Image.Image:
    """Resize, enhance, and dither an image for Spectra 6 e-ink display."""
    w, h = config.display.width, config.display.height

    img = image.convert("RGB")
    img = _resize_crop(img, w, h)

    # Boost contrast and saturation for e-ink
    img = ImageEnhance.Contrast(img).enhance(1.2)
    img = ImageEnhance.Color(img).enhance(1.3)

    img = _atkinson_dither(img, SPECTRA6_PALETTE)

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


def _atkinson_dither(
    img: Image.Image, palette: list[tuple[int, int, int]]
) -> Image.Image:
    """Atkinson dithering — diffuses 1/8 of error to 6 neighbors (75% total)."""
    pixels = np.array(img, dtype=np.float64)
    h, w, _ = pixels.shape
    pal = np.array(palette, dtype=np.float64)

    for y in range(h):
        for x in range(w):
            old = pixels[y, x].copy()
            # Find nearest palette color (Euclidean distance)
            dists = np.sum((pal - old) ** 2, axis=1)
            nearest_idx = np.argmin(dists)
            new = pal[nearest_idx]
            pixels[y, x] = new

            # Atkinson: 1/8 of error to each of 6 neighbors
            err = (old - new) / 8.0

            if x + 1 < w:
                pixels[y, x + 1] += err
            if x + 2 < w:
                pixels[y, x + 2] += err
            if y + 1 < h:
                if x - 1 >= 0:
                    pixels[y + 1, x - 1] += err
                pixels[y + 1, x] += err
                if x + 1 < w:
                    pixels[y + 1, x + 1] += err
            if y + 2 < h:
                pixels[y + 2, x] += err

    pixels = np.clip(pixels, 0, 255).astype(np.uint8)
    return Image.fromarray(pixels)
