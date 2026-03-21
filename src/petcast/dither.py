"""Spectra 6 e-ink dithering pipeline.

Uses CIELAB color space for perceptually accurate nearest-color matching.
"""

import numpy as np
from PIL import Image, ImageEnhance

from petcast.config import Config

# Spectra 6 palette — values tuned for the display driver's color mapping
SPECTRA6_PALETTE = [
    (0, 0, 0),        # Black
    (255, 255, 255),   # White
    (200, 0, 0),       # Red
    (0, 150, 0),       # Green
    (0, 0, 200),       # Blue
    (255, 230, 0),     # Yellow
]


def _srgb_to_linear(c: np.ndarray) -> np.ndarray:
    """Convert sRGB [0,255] to linear RGB [0,1]."""
    c = np.clip(c, 0, 255) / 255.0
    return np.where(c <= 0.04045, c / 12.92, ((c + 0.055) / 1.055) ** 2.4)


def _linear_to_xyz(rgb: np.ndarray) -> np.ndarray:
    """Convert linear RGB to CIE XYZ."""
    m = np.array([
        [0.4124564, 0.3575761, 0.1804375],
        [0.2126729, 0.7151522, 0.0721750],
        [0.0193339, 0.1191920, 0.9503041],
    ])
    return rgb @ m.T


def _xyz_to_lab(xyz: np.ndarray) -> np.ndarray:
    """Convert CIE XYZ to CIELAB."""
    ref = np.array([0.95047, 1.00000, 1.08883])
    xyz = xyz / ref

    def f(t):
        delta = 6 / 29
        return np.where(t > delta**3, t ** (1/3), t / (3 * delta**2) + 4/29)

    fx, fy, fz = f(xyz[..., 0]), f(xyz[..., 1]), f(xyz[..., 2])
    L = 116 * fy - 16
    a = 500 * (fx - fy)
    b = 200 * (fy - fz)
    return np.stack([L, a, b], axis=-1)


def _rgb_to_lab(rgb: np.ndarray) -> np.ndarray:
    """Convert sRGB [0,255] to CIELAB."""
    linear = _srgb_to_linear(rgb)
    xyz = _linear_to_xyz(linear)
    return _xyz_to_lab(xyz)


def dither_for_display(image: Image.Image, config: Config) -> Image.Image:
    """Resize, enhance, and dither an image for Spectra 6 e-ink display."""
    w, h = config.display.width, config.display.height

    img = image.convert("RGB")
    img = _resize_crop(img, w, h)

    # Boost contrast and saturation for e-ink
    img = ImageEnhance.Contrast(img).enhance(1.2)
    img = ImageEnhance.Color(img).enhance(1.3)

    img = _floyd_steinberg_dither(img, SPECTRA6_PALETTE)

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
    palette: list[tuple[int, int, int]],
) -> Image.Image:
    """Floyd-Steinberg dithering with CIELAB perceptual distance."""
    pixels = np.array(img, dtype=np.float64)
    h, w, _ = pixels.shape

    # Pre-convert palette to LAB
    pal_rgb = np.array(palette, dtype=np.float64)
    pal_lab = _rgb_to_lab(pal_rgb)

    for y in range(h):
        for x in range(w):
            old_rgb = pixels[y, x].copy()

            # Find nearest palette color in CIELAB space
            old_lab = _rgb_to_lab(old_rgb)
            dists = np.sum((pal_lab - old_lab) ** 2, axis=1)
            nearest_idx = np.argmin(dists)
            new_rgb = pal_rgb[nearest_idx]

            pixels[y, x] = new_rgb
            error = old_rgb - new_rgb

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
