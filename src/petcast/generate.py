"""Image generation — dispatches to OpenAI or Gemini based on config."""

import base64
import io
from datetime import datetime
from pathlib import Path
from zoneinfo import ZoneInfo

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
    """Generate an image using the configured provider."""
    prompt = _build_prompt(selection, scene, forecast, battery_pct=battery_pct)
    photo_path = root / "pets" / "input" / selection.photo
    ref_photo = photo_path if photo_path.exists() else None

    provider = config.image_provider.lower()
    if provider == "openai":
        return _generate_openai(config, prompt, ref_photo)
    if provider == "gemini":
        return _generate_gemini(config, prompt, ref_photo)
    raise ValueError(f"Unknown image_provider: {config.image_provider!r} (expected 'openai' or 'gemini')")


def _generate_openai(config: Config, prompt: str, ref_photo: Path | None) -> Image.Image:
    """Generate via OpenAI gpt-image-2. Uses images.edit when a reference photo exists."""
    from openai import OpenAI

    client = OpenAI()
    kwargs = {
        "model": config.openai.image_model,
        "prompt": prompt,
        "size": config.openai.size,
        "quality": config.openai.quality,
        "n": 1,
    }

    if ref_photo is not None:
        with open(ref_photo, "rb") as f:
            resp = client.images.edit(image=f, **kwargs)
    else:
        resp = client.images.generate(**kwargs)

    b64 = resp.data[0].b64_json
    if not b64:
        raise RuntimeError("OpenAI returned no image data")
    return Image.open(io.BytesIO(base64.b64decode(b64)))


def _generate_gemini(config: Config, prompt: str, ref_photo: Path | None) -> Image.Image:
    """Generate via Gemini image model with optional reference photo."""
    from google import genai
    from google.genai import types

    contents: list = [prompt]
    if ref_photo is not None:
        contents.append(Image.open(ref_photo))

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

    for part in response.parts:
        if part.inline_data is not None:
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
- IMPORTANT: The image will be cropped to 5:3 (800x480). Keep ALL important content \
(pets, weather info, focal points) within the center 80% of the image vertically. \
Extend backgrounds to the full edges but don't put anything important near the top or bottom.
"""
