"""Gemini image generation with pet reference photos."""

from datetime import datetime
from pathlib import Path
from zoneinfo import ZoneInfo

from google import genai
from google.genai import types
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
    """Generate an image using Gemini with pet reference photos."""
    prompt = _build_prompt(selection, scene, forecast, battery_pct=battery_pct)

    # Build contents: prompt text + reference photo
    contents: list = [prompt]
    photo_path = root / "pets" / "input" / selection.photo
    if photo_path.exists():
        contents.append(Image.open(photo_path))

    client = genai.Client()
    response = client.models.generate_content(
        model=config.gemini.image_model,
        contents=contents,
        config=types.GenerateContentConfig(
            response_modalities=["TEXT", "IMAGE"],
            image_config=types.ImageConfig(
                aspect_ratio="3:2",
            ),
        ),
    )

    # Extract image from response — convert to PIL Image
    for part in response.parts:
        if part.inline_data is not None:
            import io
            return Image.open(io.BytesIO(part.inline_data.data))

    raise RuntimeError("No image was generated in the response")


def _build_prompt(
    selection: Selection, scene: SceneDescription, forecast: Forecast,
    battery_pct: float | None = None,
) -> str:
    today = datetime.now(ZoneInfo(forecast["timezone"]))
    day_name = today.strftime("%A")
    month_name = today.strftime("%B")
    day_num = str(today.day)
    temp_str = f"{forecast['high_f']:.0f}°/{forecast['low_f']:.0f}°"

    num_pets = len(selection.pets)
    pet_count_word = {1: "one", 2: "two", 3: "three", 4: "four", 5: "five"}.get(num_pets, str(num_pets))

    pet_list = "\n".join(
        f"  {i+1}. {p.name}: {p.description}" for i, p in enumerate(selection.pets)
    )

    return f"""\
ART STYLE — THIS IS THE MOST IMPORTANT INSTRUCTION:
{selection.style}

The image MUST look like it was physically created using the medium described above. \
Not a digital illustration with a style filter — it should look like the REAL THING. \
If it says "linocut print", the image should look like ink pressed from a carved block. \
If it says "graffiti on a wall", the image should look like spray paint on concrete. \
The medium IS the image. Every pixel should reinforce the chosen art style.

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

WEATHER INFO: Creatively incorporate today's forecast into the scene. Must include:
- A weather ICON (visual, not text): {forecast['weather_icon_desc']}
- The date: {day_name}, {month_name} {day_num}
- Temperature: {temp_str}\
{f"""
- A low battery warning ({battery_pct:.0f}%)""" if battery_pct is not None and battery_pct < 15 else ""}
Integrate these naturally in whatever way fits the art style — a sign, chalkboard, \
newspaper, banner, window, sky writing, etc. Be creative.

RULES:
- Exactly {num_pets} pets, each with ONE head. No duplicates.
- No text except the weather info.
- Season and weather reflected in the environment.
- Do NOT add any border, frame, or white edge around the image.
- IMPORTANT: This image will be generated at 3:2 aspect ratio but CROPPED to 5:3 (800x480). \
That means ~10% of the top and bottom will be cut off. Keep ALL important content \
(pets, weather info, focal points) within the center 80% of the image vertically. \
Extend backgrounds to the full edges but don't put anything important near the top or bottom.
"""
