"""OpenAI image generation."""

import base64
import io
from pathlib import Path

from openai import OpenAI
from PIL import Image

from petcast.config import Config
from petcast.scene import SceneDescription
from petcast.select import Selection


def generate_image(
    config: Config,
    selection: Selection,
    scene: SceneDescription,
    root: Path,
) -> Image.Image:
    """Generate an image using OpenAI's image edit API with pet reference photos."""
    prompt = _build_prompt(selection, scene)

    # Collect pet reference photo paths
    photo_paths: list[Path] = []
    for photo_filename in selection.photos:
        photo_path = root / "pets" / "input" / photo_filename
        if photo_path.exists():
            photo_paths.append(photo_path)

    client = OpenAI()

    if photo_paths:
        # Use images.edit to pass reference photos
        files = [open(p, "rb") for p in photo_paths]
        try:
            result = client.images.edit(
                image=files if len(files) > 1 else files[0],
                prompt=prompt,
                model=config.openai.model,
                n=1,
                size=config.openai.size,
            )
        finally:
            for f in files:
                f.close()
    else:
        # No reference photos, fall back to generate
        result = client.images.generate(
            model=config.openai.model,
            prompt=prompt,
            n=1,
            size=config.openai.size,
        )

    # gpt-image models always return b64_json
    image_data = base64.standard_b64decode(result.data[0].b64_json)
    return Image.open(io.BytesIO(image_data))


def _build_prompt(selection: Selection, scene: SceneDescription) -> str:
    pet_names = " and ".join(p.name for p in selection.pets)
    pet_descs = "; ".join(
        f"{p.name} is {p.description}" for p in selection.pets
    )

    return f"""\
Create an image in the style of {selection.style}.

Subject: {pet_names} — {scene.activity}

Pet descriptions (MUST match these closely using the reference photos): {pet_descs}

Foreground: {scene.foreground}
Background: {scene.background}
Mood/lighting: {scene.mood}
Composition: {scene.constraints}

IMPORTANT: The pets should be the clear focal point. Make them recognizable and true to their \
reference photos. The scene should feel warm and inviting. Do NOT include any text or writing in the image.
"""
