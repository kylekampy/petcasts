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

SEASONAL ACCURACY: The scene must match what the landscape ACTUALLY looks like on this \
specific date — not a generic idea of the season. You will be given a phenology description \
of the current state (bare vs. budding vs. leafing out vs. full canopy vs. turning vs. falling). \
Follow it precisely. Transitions matter — mid-April trees are NOT bare AND NOT fully leafed out; \
they are just budding with tiny tender leaves. Early September is NOT peak fall — it's mostly \
green with hints of color. Match the phenology description exactly in foreground and background.

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
    season, phenology = _phenology(now.month, now.day)

    all_pet_names = ", ".join(p.name for p in selection.pets)

    user_prompt = f"""\
Pets (ALL must appear in the scene):
{pet_descriptions}

Date: {date_str}
Season: {season}
Phenology (what the landscape actually looks like RIGHT NOW — match this precisely):
{phenology}
Location: {config.location.name} (upper Midwest, ~44°N)

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

    data, _ = json.JSONDecoder().raw_decode(raw)

    return SceneDescription(
        activity=data["activity"],
        foreground=data["foreground"],
        background=data["background"],
        mood=data["mood"],
        constraints=data["constraints"],
        weather_integration=data.get("weather_integration", "on a small sign in the corner"),
    )


# Phenology calendar tuned for upper Midwest (~44°N, e.g. La Crosse WI).
# (start_month, start_day, season, description) — last entry wraps back to winter.
_PHENOLOGY_CALENDAR = [
    (1, 1,  "winter",       "Deep winter. Snow cover on the ground and branches. Trees fully bare, dark silhouettes. Grass hidden or brown where exposed. Frozen puddles, ice on water."),
    (3, 1,  "late winter",  "Late winter thaw. Patchy dirty snow, brown mud, matted dead grass. Trees still fully bare. Landscape drab — grays, browns, dull ochre."),
    (3, 21, "early spring", "Early spring. Trees STILL BARE — no leaves yet. Buds may be swelling on branches but no green. Grass just starting to green at the base, mostly still brown/tan. Maybe crocuses or early daffodils. Mud season."),
    (4, 15, "mid-spring",   "Mid-spring transition. Trees are JUST starting to leaf out — tiny tender pale-green leaves emerging from buds, branches still largely visible through sparse foliage. NOT bare, NOT full canopy. Flowering trees in bloom (redbud, crabapple, magnolia — pink/white blossoms). Grass greening up but still patchy. Daffodils and tulips."),
    (5, 1,  "late spring",  "Late spring. Trees rapidly leafing out with fresh bright yellow-green leaves, canopy filling in but still translucent. Lush new growth everywhere. Grass fully green. Flowering trees finishing, lilacs and tulips peaking."),
    (5, 21, "early summer", "Early summer. Full leaf-out, trees in fresh vibrant green, dense canopy. Grass lush and green. Gardens filling in. Long daylight."),
    (6, 16, "high summer",  "High summer. Deep mature green canopy, full dense foliage. Grass green but may show heat stress or dry patches. Wildflowers, gardens in full bloom."),
    (8, 16, "late summer",  "Late summer. Foliage still full but looking tired — dusty, darker, some yellowing on edges. Grass may be browning from heat. Fields of mature crops (corn tall, soy turning)."),
    (9, 11, "early fall",   "Early fall. First hints of color — scattered yellow and orange leaves mixed with green, especially on maples and early-turning trees. Grass still green. Crisp air, clear skies."),
    (10, 1, "peak fall",    "Peak fall color. Blazing reds, oranges, yellows across the canopy. Leaves starting to drift down. Grass still green but cooling. Pumpkins, cornstalks."),
    (10, 21,"late fall",    "Late fall. Most leaves have fallen — trees mostly bare with a few stubborn brown leaves clinging. Ground covered in fallen leaves. Grass fading to tan/brown. Gray skies common."),
    (11, 11,"early winter", "Early winter. Trees fully bare, dark wet branches. Grass brown and dormant. Possible first snow — light dusting or flurries. Overcast, raw, damp."),
    (12, 1, "winter",       "Winter. Snow likely on the ground. Trees fully bare, sometimes frosted or snow-laden. Grass hidden under snow or brown where exposed. Frozen landscape, low sun."),
]


def _phenology(month: int, day: int) -> tuple[str, str]:
    """Return (season, multi-line phenology description) for the given month/day."""
    key = (month, day)
    match = _PHENOLOGY_CALENDAR[0]
    for entry in _PHENOLOGY_CALENDAR:
        if (entry[0], entry[1]) <= key:
            match = entry
    return match[2], match[3]
