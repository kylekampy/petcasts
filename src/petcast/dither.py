"""Spectra 6 e-ink dithering pipeline."""

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
    img: Image.Image, palette: list[tuple[int, int, int]]
) -> Image.Image:
    """Apply Floyd-Steinberg dithering against a fixed palette."""
    pixels = np.array(img, dtype=np.float64)
    h, w, _ = pixels.shape
    pal = np.array(palette, dtype=np.float64)
    n_colors = len(palette)

    for y in range(h):
        row = pixels[y]
        next_row = pixels[y + 1] if y + 1 < h else None
        for x in range(w):
            r, g, b = row[x]

            # Find nearest palette color (Euclidean distance, inlined)
            best_idx = 0
            best_dist = float("inf")
            for i in range(n_colors):
                dr = r - pal[i, 0]
                dg = g - pal[i, 1]
                db = b - pal[i, 2]
                d = dr * dr + dg * dg + db * db
                if d < best_dist:
                    best_dist = d
                    best_idx = i

            nr, ng, nb = pal[best_idx]
            er, eg, eb = r - nr, g - ng, b - nb
            row[x, 0] = nr
            row[x, 1] = ng
            row[x, 2] = nb

            if x + 1 < w:
                row[x + 1, 0] += er * 0.4375
                row[x + 1, 1] += eg * 0.4375
                row[x + 1, 2] += eb * 0.4375
            if next_row is not None:
                if x - 1 >= 0:
                    next_row[x - 1, 0] += er * 0.1875
                    next_row[x - 1, 1] += eg * 0.1875
                    next_row[x - 1, 2] += eb * 0.1875
                next_row[x, 0] += er * 0.3125
                next_row[x, 1] += eg * 0.3125
                next_row[x, 2] += eb * 0.3125
                if x + 1 < w:
                    next_row[x + 1, 0] += er * 0.0625
                    next_row[x + 1, 1] += eg * 0.0625
                    next_row[x + 1, 2] += eb * 0.0625

    pixels = np.clip(pixels, 0, 255).astype(np.uint8)
    return Image.fromarray(pixels)
