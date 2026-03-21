"""Spectra 6 e-ink dithering pipeline.

Uses calibrated display colors for dithering decisions, then remaps to
reference palette values that the display driver expects.
"""

import numpy as np
from PIL import Image, ImageEnhance

from petcast.config import Config

# What the colors ACTUALLY look like on the Spectra 6 display (calibrated)
# Used for dithering decisions (nearest-color matching)
DISPLAY_COLORS = [
    (0, 0, 0),          # Black
    (255, 255, 255),     # White
    (160, 32, 32),       # Red (muted, not pure)
    (96, 128, 80),       # Green (olive-toned)
    (80, 128, 184),      # Blue (shifted toward cyan)
    (240, 224, 80),      # Yellow (warm)
]

# What the display driver expects in the PNG (reference values)
# The driver maps these to the actual e-ink pigments
REFERENCE_PALETTE = [
    (0, 0, 0),          # Black
    (255, 255, 255),     # White
    (255, 0, 0),         # Red
    (0, 255, 0),         # Green
    (0, 0, 255),         # Blue
    (255, 255, 0),       # Yellow
]


def dither_for_display(image: Image.Image, config: Config) -> Image.Image:
    """Resize, enhance, and dither an image for Spectra 6 e-ink display."""
    w, h = config.display.width, config.display.height

    img = image.convert("RGB")
    img = _resize_crop(img, w, h)

    # Boost contrast and saturation for e-ink
    img = ImageEnhance.Contrast(img).enhance(1.2)
    img = ImageEnhance.Color(img).enhance(1.3)

    # Dither against calibrated display colors, then remap to reference palette
    img = _floyd_steinberg_dither(img, DISPLAY_COLORS, REFERENCE_PALETTE)

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


def _floyd_steinberg_dither(
    img: Image.Image,
    display_colors: list[tuple[int, int, int]],
    reference_palette: list[tuple[int, int, int]],
) -> Image.Image:
    """Dither using display colors for decisions, output reference palette values."""
    pixels = np.array(img, dtype=np.float64)
    h, w, _ = pixels.shape
    display = np.array(display_colors, dtype=np.float64)
    reference = np.array(reference_palette, dtype=np.uint8)

    for y in range(h):
        for x in range(w):
            old = pixels[y, x].copy()
            # Find nearest DISPLAY color (what it actually looks like)
            dists = np.sum((display - old) ** 2, axis=1)
            nearest_idx = np.argmin(dists)

            # Error diffusion uses display color (perceptually accurate)
            error = old - display[nearest_idx]

            # But write the REFERENCE color (what the driver expects)
            pixels[y, x] = reference[nearest_idx]

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
