"""Structured scene prompt generation via OpenAI chat."""

import json
from dataclasses import dataclass
from datetime import datetime
from zoneinfo import ZoneInfo

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
today's weather, the date and season, and an art style, you design a charming scene \
featuring ALL the pets together in a weather-appropriate and seasonally-accurate activity.

The scene must be seasonally accurate for the location. For example, in the upper Midwest \
in early March, trees are bare, grass is brown/dormant, and there may be leftover snow. \
Do not depict lush green landscapes when the season doesn't support it.

The final image will include a small weather forecast panel rendered in the chosen art style. \
You must decide where this panel goes based on the scene composition — pick the corner with \
the least visual interest. The image will be cropped slightly on the top and bottom, so \
keep all important elements (pets, panel) in the central 80% vertically.

Respond with ONLY a JSON object (no markdown fencing) with these keys:
- activity: what the pets are doing together (1 sentence, must include ALL pets)
- foreground: detailed foreground description for the image generator
- background: detailed background/setting description (must be seasonally accurate)
- mood: lighting and color mood
- constraints: important composition notes (e.g. "leave clear space in bottom-right for weather panel")
- overlay_position: where the weather panel should go: "top-left", "top-right", "bottom-left", or "bottom-right"
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

    now = datetime.now(ZoneInfo(forecast["timezone"]))
    date_str = now.strftime("%B %-d, %Y")
    month = now.month
    if month in (3, 4, 5):
        season = "spring"
    elif month in (6, 7, 8):
        season = "summer"
    elif month in (9, 10, 11):
        season = "fall/autumn"
    else:
        season = "winter"

    all_pet_names = ", ".join(p.name for p in selection.pets)

    user_prompt = f"""\
Pets (ALL must appear in the scene):
{pet_descriptions}

Date: {date_str}
Season: {season}
Location: {config.location.name}

Weather: {forecast['weather_desc']}, high {forecast['high_f']:.0f}°F / low {forecast['low_f']:.0f}°F, \
{forecast['precip_chance']}% chance of precipitation, wind {forecast['wind_mph']:.0f} mph.
Sunrise: {forecast['sunrise']}, Sunset: {forecast['sunset']}

Art style: {selection.style}

Recent scenes to AVOID repeating:
{json.dumps(recent_activities) if recent_activities else "(none yet)"}

Design a scene featuring ALL pets ({all_pet_names}) for today's forecast image. \
The environment must reflect the actual season and weather at this location.
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
