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

    num_pets = len(selection.pets)
    pet_count_word = {1: "one", 2: "two", 3: "three", 4: "four", 5: "five"}.get(num_pets, str(num_pets))

    # Build individual pet descriptions with emphasis on uniqueness
    pet_list = "\n".join(
        f"  {i+1}. {p.name}: {p.description}" for i, p in enumerate(selection.pets)
    )

    return f"""\
Create a wide landscape illustration in the style of {selection.style}.

The image should look authentically like {selection.style} — use the specific visual \
techniques, textures, line qualities, and color treatments of that medium. Do not just \
apply a filter; make it look like it was genuinely created in this style.

SCENE: {scene.activity}

EXACTLY {pet_count_word.upper()} ({num_pets}) PETS — no more, no less. Each appears ONCE:
{pet_list}

Use the reference photo to match each pet's appearance. Every pet has exactly ONE head \
and ONE body. Do NOT duplicate any pet. Count the pets in your output — there must be \
exactly {num_pets}.

The pets are anthropomorphic — they can hold objects, sit in chairs, wear accessories, \
and do human activities. They should look charming and whimsical.

FOREGROUND: {scene.foreground}
BACKGROUND: {scene.background}
MOOD: {scene.mood}
COMPOSITION: {scene.constraints}

WEATHER PANEL in the {scene.overlay_position} corner, rendered in the {selection.style} style:
- Weather icon for "{weather_summary}" ({forecast['precip_chance']}% precip — \
{"NO rain in the icon, just clouds" if forecast['precip_chance'] < 30 else "show rain"})
- "{day_name}, {month_name} {day_num}" (spell exactly: {day_spelled})
- "{temp_str}"\
{f"""
- LOW BATTERY icon: {battery_pct:.0f}%""" if battery_pct is not None and battery_pct < 15 else ""}
Inset the panel 15% from top/bottom edges, 5% from sides (image will be cropped to wider ratio).

RULES:
- Exactly {num_pets} pets, each with ONE head. No duplicates.
- No text except the weather panel.
- Season and weather reflected in the environment.
- Keep all content away from top/bottom edges (will be cropped ~7%).
"""
