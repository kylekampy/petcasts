from datetime import datetime, timedelta
from pathlib import Path
from zoneinfo import ZoneInfo

from petcast.config import (
    CelebrationConfig,
    Config,
    CooldownConfig,
    DisplayConfig,
    GeminiConfig,
    LocationConfig,
    OpenAIConfig,
    OutputConfig,
    Pet,
)
from petcast.scene import SYSTEM_PROMPT, generate_scene
from petcast.select import Selection


def _config(pets):
    return Config(
        location=LocationConfig("Test", 0.0, 0.0),
        styles=["style"],
        image_provider="openai",
        scene_provider="openai",
        gemini=GeminiConfig("gemini-image", "gemini-chat"),
        openai=OpenAIConfig("openai-image", "medium", "1536x1024", "openai-chat"),
        display=DisplayConfig(800, 480),
        output=OutputConfig(
            latest=Path("output/latest.png"),
            latest_raw=Path("output/latest_raw.png"),
            metadata=Path("output/latest.json"),
            debug_dir=Path("output/debug"),
            archive_dir=Path("output/archive"),
            daily_dir=Path("output/daily"),
        ),
        cooldowns=CooldownConfig(7, 14, 7),
        celebrations=CelebrationConfig(),
        pets=pets,
    )


def test_scene_prompt_bans_weather_presenter_compositions():
    assert "not weather presenters" in SYSTEM_PROMPT
    assert "Do NOT make a pet point at" in SYSTEM_PROMPT
    assert "Avoid defaulting to pets gathered around a table/chairs" in SYSTEM_PROMPT
    assert "pool-noodle joust" in SYSTEM_PROMPT
    assert "paper-boat regatta" in SYSTEM_PROMPT
    assert "emergency biscuit tin" in SYSTEM_PROMPT


def test_generate_scene_includes_only_last_week_scene_avoidance(monkeypatch):
    captured = {}

    def fake_chat(config, user_prompt):
        captured["prompt"] = user_prompt
        return """
        {
          "activity": "Alice explores the garden.",
          "foreground": "Alice trots along broad stepping stones.",
          "background": "A simple leafy yard.",
          "mood": "Bright and clear.",
          "constraints": "Keep Alice centered.",
          "weather_integration": "Forecast painted on garden stones."
        }
        """

    monkeypatch.setattr("petcast.scene._chat_openai", fake_chat)
    pet = Pet("Alice", "gray cat", ["alice.png"])
    forecast = {
        "weather_desc": "Clear sky",
        "high_f": 70.0,
        "low_f": 50.0,
        "precip_chance": 0,
        "wind_mph": 5.0,
        "sunrise": "2026-04-29T06:00",
        "sunset": "2026-04-29T20:00",
        "timezone": "America/Chicago",
    }
    now = datetime.now(ZoneInfo("America/Chicago"))
    history = [
        {
            "date": (now - timedelta(days=8)).isoformat(),
            "scene": {
                "activity": "Alice runs an old umbrella parade.",
                "weather_integration": "Forecast painted on an old umbrella.",
            },
        },
        {
            "date": (now - timedelta(days=1)).isoformat(),
            "scene": {
                "activity": "Alice reads a forecast card.",
                "weather_integration": "Forecast on a card on a table.",
            }
        }
    ]

    generate_scene(_config([pet]), Selection([pet], "alice.png", "style"), forecast, history)

    assert "Recent scene patterns from the last 7 days to AVOID repeating" in captured["prompt"]
    assert "Do not repeat any activity from the last 7 days" in captured["prompt"]
    assert "Alice reads a forecast card." in captured["prompt"]
    assert "Forecast on a card on a table." in captured["prompt"]
    assert "Alice runs an old umbrella parade." not in captured["prompt"]
