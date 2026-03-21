"""OpenAI image generation."""

import base64
import io
from datetime import datetime
from pathlib import Path
from zoneinfo import ZoneInfo

from openai import OpenAI
from PIL import Image

from petcast.config import Config
from petcast.scene import SceneDescription
from petcast.select import Selection
from petcast.weather import Forecast


def generate_image(
    config: Config,
    selection: Selection,
    scene: SceneDescription,
    forecast: Forecast,
    root: Path,
    battery_pct: float | None = None,
) -> Image.Image:
    """Generate an image using OpenAI's image API with pet reference photos."""
    prompt = _build_prompt(selection, scene, forecast, battery_pct=battery_pct)

    photo_path = root / "pets" / "input" / selection.photo
    client = OpenAI()

    if photo_path.exists():
        with open(photo_path, "rb") as f:
            result = client.images.edit(
                model=config.openai.model,
                image=[f],
                prompt=prompt,
                size=config.openai.size,
                input_fidelity="high",
            )
    else:
        result = client.images.generate(
            model=config.openai.model,
            prompt=prompt,
            size=config.openai.size,
        )

    image_data = base64.standard_b64decode(result.data[0].b64_json)
    return Image.open(io.BytesIO(image_data))


def _build_prompt(
    selection: Selection, scene: SceneDescription, forecast: Forecast,
    battery_pct: float | None = None,
) -> str:
    pet_names = ", ".join(p.name for p in selection.pets)
    pet_descs = "; ".join(
        f"{p.name}: {p.description}" for p in selection.pets
    )

    today = datetime.now(ZoneInfo(forecast["timezone"]))
    day_name = today.strftime("%A")
    day_spelled = " ".join(day_name.upper())
    month_name = today.strftime("%B")
    day_num = str(today.day)
    temp_str = f"{forecast['high_f']:.0f}°/{forecast['low_f']:.0f}°"
    weather_summary = forecast["weather_desc"]

    return f"""\
Create a wide landscape image in the style of {selection.style}.

Subject: {pet_names} — {scene.activity}

Pet descriptions (MUST match these closely using the reference photo — ALL pets must appear, \
each pet has exactly ONE head): {pet_descs}

Foreground: {scene.foreground}
Background: {scene.background}
Mood/lighting: {scene.mood}
Composition: {scene.constraints}

WEATHER FORECAST PANEL: Include a small forecast panel in the {scene.overlay_position} area, \
rendered in the same {selection.style} art style as the rest of the image. The panel should show:
- A stylized weather icon for "{weather_summary}" ({forecast['precip_chance']}% chance of rain — \
{"DO NOT show rain or raindrops in the icon, just clouds" if forecast['precip_chance'] < 30 else "rain is likely, show rain in the icon"})
- The text "{day_name}, {month_name} {day_num}" (spell it exactly: {day_spelled})
- The text "{temp_str}"\
{f"""
- A small battery icon showing approximately {battery_pct:.0f}% charge""" if battery_pct is not None else ""}
The panel should feel integrated into the artwork. Keep the text large enough to read clearly. \
Double-check spelling of all words. \
IMPORTANT: The image will be cropped to a wider aspect ratio — the top and bottom ~7% will be cut off. \
Inset the panel at least 15% from the top and bottom edges, and 5% from the left and right edges. \
Nothing important should be near the top or bottom edge.

IMPORTANT: ALL pets ({pet_names}) must appear — each with exactly ONE head. \
They should be the clear focal point. Make them recognizable and true to the reference photo. \
The weather and season must be reflected in the environment. \
Keep all important content (pets, panel) well away from the top and bottom edges. \
Do NOT include any other text besides what's in the forecast panel.
"""
