"""Forecast overlay compositing with Pillow."""

from datetime import datetime
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont

from petcast.config import Config
from petcast.scene import SceneDescription
from petcast.weather import Forecast


# Position offsets: (x_anchor, y_anchor) as fraction of image size
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
    """Composite a weather forecast panel onto the image."""
    img = image.copy()
    draw = ImageDraw.Draw(img)

    # Try to load a nice font, fall back to default
    font_large = _load_font(28)
    font_medium = _load_font(20)
    font_small = _load_font(16)

    # Build text lines
    today = datetime.now()
    date_str = today.strftime("%A, %B ") + str(today.day)
    temp_str = f"{forecast['high_f']:.0f}° / {forecast['low_f']:.0f}°"
    weather_str = f"{forecast['weather_icon']} {forecast['weather_desc']}"
    precip_str = f"{forecast['precip_chance']}% precip"

    lines = [
        (date_str, font_medium),
        (temp_str, font_large),
        (weather_str, font_medium),
        (precip_str, font_small),
    ]

    # Calculate panel size
    padding = 12
    line_spacing = 6
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

    total_height -= line_spacing  # remove trailing spacing
    panel_w = max_width + padding * 2
    panel_h = total_height

    # Position the panel
    pos = scene.overlay_position
    if pos not in POSITIONS:
        pos = "bottom-right"

    anchor_x_frac, anchor_y_frac = POSITIONS[pos]
    anchor_x = int(img.width * anchor_x_frac)
    anchor_y = int(img.height * anchor_y_frac)

    # Adjust for anchor point
    if "right" in pos:
        panel_x = anchor_x - panel_w
    else:
        panel_x = anchor_x

    if "bottom" in pos:
        panel_y = anchor_y - panel_h
    else:
        panel_y = anchor_y

    # Draw semi-transparent background
    overlay = Image.new("RGBA", img.size, (0, 0, 0, 0))
    overlay_draw = ImageDraw.Draw(overlay)
    overlay_draw.rounded_rectangle(
        [panel_x, panel_y, panel_x + panel_w, panel_y + panel_h],
        radius=10,
        fill=(0, 0, 0, 140),
    )

    # Composite the panel background
    if img.mode != "RGBA":
        img = img.convert("RGBA")
    img = Image.alpha_composite(img, overlay)
    draw = ImageDraw.Draw(img)

    # Draw text
    y = panel_y + padding
    for (text, font), (w, h) in zip(lines, line_sizes):
        # Center text within panel
        x = panel_x + (panel_w - w) // 2
        # Draw shadow
        draw.text((x + 1, y + 1), text, fill=(0, 0, 0, 200), font=font)
        # Draw text
        draw.text((x, y), text, fill=(255, 255, 255, 240), font=font)
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
