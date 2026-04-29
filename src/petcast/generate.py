"""Image generation — dispatches to OpenAI or Gemini based on config."""

import base64
import io
from contextlib import ExitStack
from datetime import datetime
from pathlib import Path
from zoneinfo import ZoneInfo

from PIL import Image

from petcast.celebrations import ActiveCelebration, image_prompt_block
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
    celebrations: list[ActiveCelebration] | None = None,
) -> Image.Image:
    """Generate an image using the configured provider."""
    ref_photos = reference_photo_paths(config, selection, root)
    prompt = _build_prompt(
        selection,
        scene,
        forecast,
        battery_pct=battery_pct,
        celebrations=celebrations,
        reference_count=len(ref_photos),
    )

    provider = config.image_provider.lower()
    if provider == "openai":
        return _generate_openai(config, prompt, ref_photos)
    if provider == "gemini":
        return _generate_gemini(config, prompt, ref_photos)
    raise ValueError(f"Unknown image_provider: {config.image_provider!r} (expected 'openai' or 'gemini')")


def reference_photo_paths(config: Config, selection: Selection, root: Path) -> list[Path]:
    """Return existing reference images that best cover the selected pets."""
    input_dir = root / "pets" / "input"
    refs: list[str] = []

    if selection.photo:
        refs.append(selection.photo)

    target_names = {pet.name for pet in selection.pets}
    existing_refs = [photo for photo in refs if (input_dir / photo).is_file()]
    covered = _pet_names_for_photos(config, existing_refs) & target_names
    uncovered = set(target_names - covered)

    photo_to_names = _photo_pet_index(config)
    while uncovered:
        candidates = []
        for photo, names in photo_to_names.items():
            if photo in refs:
                continue
            newly_covered = names & uncovered
            if not newly_covered:
                continue
            extra_pets = len(names - target_names)
            candidates.append((-len(newly_covered), extra_pets, photo))
        if not candidates:
            break
        _, _, photo = sorted(candidates)[0]
        refs.append(photo)
        uncovered -= photo_to_names[photo]

    return [input_dir / photo for photo in refs if (input_dir / photo).is_file()]


def _generate_openai(config: Config, prompt: str, ref_photos: list[Path]) -> Image.Image:
    """Generate via OpenAI gpt-image-2. Uses images.edit when reference photos exist."""
    from openai import OpenAI

    client = OpenAI()
    kwargs = {
        "model": config.openai.image_model,
        "prompt": prompt,
        "size": config.openai.size,
        "quality": config.openai.quality,
        "n": 1,
    }

    if ref_photos:
        with ExitStack() as stack:
            files = [stack.enter_context(open(path, "rb")) for path in ref_photos]
            resp = client.images.edit(image=files if len(files) > 1 else files[0], **kwargs)
    else:
        resp = client.images.generate(**kwargs)

    b64 = resp.data[0].b64_json
    if not b64:
        raise RuntimeError("OpenAI returned no image data")
    return Image.open(io.BytesIO(base64.b64decode(b64)))


def _generate_gemini(config: Config, prompt: str, ref_photos: list[Path]) -> Image.Image:
    """Generate via Gemini image model with optional reference photos."""
    from google import genai
    from google.genai import types

    contents: list = [prompt]
    for ref_photo in ref_photos:
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
    celebrations: list[ActiveCelebration] | None = None,
    reference_count: int = 0,
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
    if reference_count == 1:
        reference_instruction = "Use the reference photo to match each pet's appearance."
    elif reference_count > 1:
        reference_instruction = (
            f"Use the {reference_count} reference photos to match each pet's appearance. "
            "Reference photos may contain overlapping subsets of the pets; use them for likeness only."
        )
    else:
        reference_instruction = (
            "No reference photo is provided; use the pet descriptions to match each pet's appearance."
        )
    celebration_block = image_prompt_block(celebrations or [])
    celebration_text_rule = (
        " and the celebration text listed above"
        if celebration_block
        else ""
    )

    return f"""\
ART STYLE — THIS IS THE MOST IMPORTANT INSTRUCTION:
{selection.style}

The image MUST look like it was physically created using the medium described above. \
Not a digital illustration with a style filter — it should look like the REAL THING. \
If it says "linocut print", the image should look like ink pressed from a carved block. \
If it says "graffiti on a wall", the image should look like spray paint on concrete. \
The medium IS the image. Every pixel should reinforce the chosen art style.

DISPLAY TARGET — ALSO CRITICAL:
The final image will be resized to 800x480 and Atkinson-dithered to a six-color e-ink \
palette: black, white, red, green, blue, and yellow. Design for that final display, not \
for a high-resolution screen. Use bold simple shapes, thick outlines, large readable \
lettering, high contrast, clear silhouettes, and a small number of major focal objects. \
Avoid tiny text, thin lines, delicate textures, dense clutter, subtle gradients, soft \
shadows, low-contrast details, and small decorative background elements that will turn \
to noise after dithering.

SCENE: {scene.activity}

EXACTLY {pet_count_word.upper()} ({num_pets}) PETS — no more, no less. Each appears ONCE:
{pet_list}

{reference_instruction} Every pet has exactly ONE head \
and ONE body. Do NOT duplicate any pet. Count the pets in your output — there must be \
exactly {num_pets}.

The pets are anthropomorphic — they can hold objects, sit in chairs, wear accessories, \
and do human activities. They should look charming and whimsical.

FOREGROUND: {scene.foreground}
BACKGROUND: {scene.background}
MOOD: {scene.mood}
COMPOSITION: {scene.constraints}

{celebration_block + chr(10) if celebration_block else ""}\
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
- No text except the weather info{celebration_text_rule}.
- Weather and celebration text must be large, simple, high-contrast, and readable at 800x480.
- Prefer chunky graphic composition over intricate detail; avoid gradients and fine texture.
- Season and weather reflected in the environment.
- Do NOT add any border, frame, or white edge around the image.
- IMPORTANT: The image will be cropped to 5:3 (800x480). Keep ALL important content \
(pets, weather info, focal points) within the center 80% of the image vertically. \
Extend backgrounds to the full edges but don't put anything important near the top or bottom.
"""


def _photo_pet_index(config: Config) -> dict[str, set[str]]:
    photo_to_names: dict[str, set[str]] = {}
    for pet in config.pets:
        for photo in pet.photos:
            photo_to_names.setdefault(photo, set()).add(pet.name)
    return photo_to_names


def _pet_names_for_photos(config: Config, photos: list[str]) -> set[str]:
    photo_to_names = _photo_pet_index(config)
    names: set[str] = set()
    for photo in photos:
        names |= photo_to_names.get(photo, set())
    return names
