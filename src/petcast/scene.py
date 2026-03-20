"""Structured scene prompt generation via OpenAI chat."""

import json
from dataclasses import dataclass

from openai import OpenAI

from petcast.config import Config
from petcast.select import Selection
from petcast.weather import Forecast


@dataclass
class SceneDescription:
    activity: str
    foreground: str
    background: str
    mood: str
    constraints: str
    overlay_position: str  # "top-left", "top-right", "bottom-left", "bottom-right"


SYSTEM_PROMPT = """\
You are a creative director for a pet weather forecast display. Given pet descriptions, \
today's weather, and an art style, you design a charming scene featuring the pets in a \
weather-appropriate activity.

Respond with ONLY a JSON object (no markdown fencing) with these keys:
- activity: what the pets are doing (1 sentence)
- foreground: detailed foreground description for the image generator
- background: detailed background/setting description
- mood: lighting and color mood
- constraints: important composition notes (e.g. "leave clear space in bottom-right for weather overlay")
- overlay_position: where the weather info should go: "top-left", "top-right", "bottom-left", or "bottom-right" \
(pick whichever area has the least visual interest)
"""


def generate_scene(
    config: Config,
    selection: Selection,
    forecast: Forecast,
    history: list[dict],
) -> SceneDescription:
    """Use OpenAI chat to generate a structured scene description."""
    pet_descriptions = "\n".join(
        f"- {pet.name}: {pet.description}" for pet in selection.pets
    )

    recent_activities = []
    for entry in history[-5:]:
        if "scene" in entry:
            recent_activities.append(entry["scene"].get("activity", ""))

    user_prompt = f"""\
Pets:
{pet_descriptions}

Weather: {forecast['weather_desc']}, high {forecast['high_f']:.0f}°F / low {forecast['low_f']:.0f}°F, \
{forecast['precip_chance']}% chance of precipitation, wind {forecast['wind_mph']:.0f} mph.

Art style: {selection.style}

Recent scenes to AVOID repeating:
{json.dumps(recent_activities) if recent_activities else "(none yet)"}

Design a scene for today's forecast image.
"""

    client = OpenAI()
    resp = client.chat.completions.create(
        model=config.openai.chat_model,
        messages=[
            {"role": "system", "content": SYSTEM_PROMPT},
            {"role": "user", "content": user_prompt},
        ],
        temperature=1.0,
    )

    raw = resp.choices[0].message.content.strip()
    # Strip markdown fencing if present
    if raw.startswith("```"):
        raw = raw.split("\n", 1)[1]
        if raw.endswith("```"):
            raw = raw[: raw.rfind("```")]
        raw = raw.strip()

    data = json.loads(raw)

    return SceneDescription(
        activity=data["activity"],
        foreground=data["foreground"],
        background=data["background"],
        mood=data["mood"],
        constraints=data["constraints"],
        overlay_position=data.get("overlay_position", "bottom-right"),
    )
