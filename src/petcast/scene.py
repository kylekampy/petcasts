"""Structured scene prompt generation via Gemini."""

import json
from dataclasses import dataclass
from datetime import datetime
from zoneinfo import ZoneInfo

from google import genai

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
    weather_integration: str


SYSTEM_PROMPT = """\
You are a creative director for an illustrated pet weather forecast displayed on a small \
e-ink frame. You design charming, anthropomorphic scenes where the pets act like little \
people — doing human activities appropriate to the weather and season.

ANTHROPOMORPHISM: The pets should be doing human-like things. NOT just sitting, lying, \
or walking. Think: reading a book on the porch, sipping from a mug, grilling in the \
backyard, building a snowman, flying a kite, having a tea party, fishing, \
gardening, stargazing, playing board games. They can hold objects, wear accessories, \
sit in chairs, use tools. Make it whimsical and heartwarming.

IMPORTANT: The art style describes HOW the image is rendered, not WHAT the pets are doing. \
The pets are the SUBJECTS depicted in that style — they are NOT creating art or doing \
anything related to the style itself. For example, "graffiti style" means the image looks \
like spray paint on a wall, NOT that the pets are painting graffiti.

SEASONAL ACCURACY: The scene must match the actual season and weather at the location. \
In the upper Midwest in early spring, trees are bare, grass is brown/dormant. In summer, \
everything is lush and green. In fall, leaves are changing. In winter, there's snow. \
Don't depict lush green when the season doesn't support it.

ART STYLE: You will be given a specific art style. In your descriptions, lean HEAVILY \
into the visual characteristics of that style. Describe specific techniques, textures, \
and visual elements unique to that medium. For example, for "linocut print" describe \
bold carved lines, ink texture, woodgrain showing through. For "stained glass" describe \
thick black leading, jewel-toned glass segments, light shining through.

COMPOSITION: The image will be cropped to a wider aspect ratio — top and bottom ~7% \
will be cut off. Keep all important elements in the central 70% vertically. \
The weather info will be creatively integrated into the scene (not necessarily a panel).

CRITICAL RULES:
- Each pet appears EXACTLY ONCE. Never duplicate a pet.
- Each pet has exactly ONE head.
- Name every pet by name in the activity description.
- No border or frame around the image.

Respond with ONLY a JSON object (no markdown fencing) with these keys:
- activity: what the pets are doing (1 sentence, must name ALL pets, must be anthropomorphic)
- foreground: detailed foreground description leaning heavily into the art style's visual language
- background: detailed background (seasonally accurate, described in the art style's visual language)
- mood: lighting and color mood specific to the art style
- constraints: composition notes
- weather_integration: creative idea for how to show the weather info in the scene (e.g. "on a chalkboard sign", "written in clouds", "on a newspaper the cat is reading")
"""


def generate_scene(
    config: Config,
    selection: Selection,
    forecast: Forecast,
    history: list[dict],
    battery_pct: float | None = None,
) -> SceneDescription:
    """Use Gemini to generate a structured scene description."""
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

Design an anthropomorphic scene featuring ALL pets ({all_pet_names}) for today's forecast image. \
The pets should be doing something human-like and charming that fits the weather. \
The environment must reflect the actual season and weather at this location. \
Lean heavily into the visual language of the "{selection.style}" art style in your descriptions.
"""

    if battery_pct is not None and battery_pct < 15:
        user_prompt += f"""
IMPORTANT: The display frame's battery is critically low at {battery_pct:.0f}%! \
The pets should look worried, anxious, or concerned about running out of energy. \
Maybe they're huddled around a dying campfire, or looking at a dimming lantern, \
or one of them is holding a nearly-empty battery. Make the low energy theme \
a charming but noticeable part of the scene.
"""

    client = genai.Client()
    resp = client.models.generate_content(
        model=config.gemini.chat_model,
        contents=f"{SYSTEM_PROMPT}\n\n{user_prompt}",
    )

    raw = resp.text.strip()
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
        weather_integration=data.get("weather_integration", "on a small sign in the corner"),
    )
