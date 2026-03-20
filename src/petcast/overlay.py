"""Forecast overlay compositing with Pillow.

Designed to be applied AFTER dithering, so uses only Spectra 6 palette colors
with no anti-aliasing or transparency.
"""

from datetime import datetime
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont

from petcast.config import Config
from petcast.dither import SPECTRA6_PALETTE
from petcast.scene import SceneDescription
from petcast.weather import Forecast

BLACK = SPECTRA6_PALETTE[0]
WHITE = SPECTRA6_PALETTE[1]

POSITIONS = {
    "top-left": (0.02, 0.02),
    "top-right": (0.98, 0.02),
    "bottom-left": (0.02, 0.98),
    "bottom-right": (0.98, 0.98),
}


def composite_overlay(
    image: Image.Image,
    forecast: Forecast,
    scene: SceneDescription,
    config: Config,
) -> Image.Image:
    """Composite a weather forecast panel using only palette colors."""
    img = image.copy().convert("RGB")
    draw = ImageDraw.Draw(img)

    font_large = _load_font(28)
    font_medium = _load_font(20)
    font_small = _load_font(16)

    today = datetime.now()
    date_str = today.strftime("%A, %B ") + str(today.day)
    temp_str = f"{forecast['high_f']:.0f}' / {forecast['low_f']:.0f}'"
    weather_str = forecast["weather_desc"]
    precip_str = f"{forecast['precip_chance']}% precip"

    lines = [
        (date_str, font_medium),
        (temp_str, font_large),
        (weather_str, font_medium),
        (precip_str, font_small),
    ]

    # Calculate panel size
    padding = 10
    line_spacing = 4
    max_width = 0
    total_height = padding * 2

    line_sizes = []
    for text, font in lines:
        bbox = draw.textbbox((0, 0), text, font=font)
        w = bbox[2] - bbox[0]
        h = bbox[3] - bbox[1]
        line_sizes.append((w, h))
        max_width = max(max_width, w)
        total_height += h + line_spacing

    total_height -= line_spacing
    panel_w = max_width + padding * 2
    panel_h = total_height

    # Position the panel
    pos = scene.overlay_position
    if pos not in POSITIONS:
        pos = "bottom-right"

    anchor_x_frac, anchor_y_frac = POSITIONS[pos]
    anchor_x = int(img.width * anchor_x_frac)
    anchor_y = int(img.height * anchor_y_frac)

    if "right" in pos:
        panel_x = anchor_x - panel_w
    else:
        panel_x = anchor_x

    if "bottom" in pos:
        panel_y = anchor_y - panel_h
    else:
        panel_y = anchor_y

    # Solid black background — no transparency, no rounded corners
    draw.rectangle(
        [panel_x, panel_y, panel_x + panel_w, panel_y + panel_h],
        fill=BLACK,
    )

    # Draw white text, no anti-aliasing
    y = panel_y + padding
    for (text, font), (w, h) in zip(lines, line_sizes):
        x = panel_x + (panel_w - w) // 2
        draw.text((x, y), text, fill=WHITE, font=font)
        y += h + line_spacing

    return img


def _load_font(size: int) -> ImageFont.FreeTypeFont | ImageFont.ImageFont:
    """Try to load a system font, fall back to default."""
    font_paths = [
        "/System/Library/Fonts/Helvetica.ttc",
        "/System/Library/Fonts/SFCompact.ttf",
        "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
        "/usr/share/fonts/TTF/DejaVuSans.ttf",
    ]
    for path in font_paths:
        if Path(path).exists():
            try:
                return ImageFont.truetype(path, size)
            except OSError:
                continue
    return ImageFont.load_default(size)
