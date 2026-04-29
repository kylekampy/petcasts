from pathlib import Path

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
from petcast.generate import _build_prompt, reference_photo_paths
from petcast.scene import SceneDescription
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


def _touch_inputs(root, *names):
    input_dir = root / "pets" / "input"
    input_dir.mkdir(parents=True)
    for name in names:
        (input_dir / name).write_bytes(b"fake image data")


def test_reference_photo_paths_cover_all_pets_with_multiple_images(tmp_path):
    pets = [
        Pet("Margot", "dog", ["current-pack.png"]),
        Pet("Zeya", "dog", ["current-pack.png", "old-pack.png"]),
        Pet("Pebble", "dog", ["current-pack.png"]),
        Pet("Mumford", "cat", ["current-pack.png"]),
        Pet("Fennec", "cat", ["current-pack.png"]),
        Pet("Patrick", "cat", ["old-pack.png"]),
        Pet("Pixel", "cat", ["old-pack.png"]),
        Pet("Kona", "cat", ["old-pack.png"]),
    ]
    _touch_inputs(tmp_path, "current-pack.png", "old-pack.png")
    selection = Selection(pets=pets, photo=None, style="style")

    refs = reference_photo_paths(_config(pets), selection, tmp_path)

    assert [path.name for path in refs] == ["current-pack.png", "old-pack.png"]


def test_reference_photo_paths_keeps_selected_photo_when_it_covers_everyone(tmp_path):
    pets = [
        Pet("Alice", "cat", ["alice-bob.png"]),
        Pet("Bob", "dog", ["alice-bob.png"]),
    ]
    _touch_inputs(tmp_path, "alice-bob.png", "alice.png")
    selection = Selection(pets=pets, photo="alice-bob.png", style="style")

    refs = reference_photo_paths(_config(pets), selection, tmp_path)

    assert [path.name for path in refs] == ["alice-bob.png"]


def test_reference_photo_paths_adds_supplemental_photos_for_uncovered_pets(tmp_path):
    pets = [
        Pet("Alice", "cat", ["alice.png"]),
        Pet("Bob", "dog", ["bob.png"]),
    ]
    _touch_inputs(tmp_path, "alice.png", "bob.png")
    selection = Selection(pets=pets, photo="alice.png", style="style")

    refs = reference_photo_paths(_config(pets), selection, tmp_path)

    assert [path.name for path in refs] == ["alice.png", "bob.png"]


def test_build_prompt_includes_eink_display_constraints():
    pets = [Pet("Alice", "gray cat", ["alice.png"])]
    selection = Selection(pets=pets, photo="alice.png", style="bold poster style")
    scene = SceneDescription(
        activity="Alice reads the forecast.",
        foreground="Alice sits by a big sign.",
        background="Simple spring yard.",
        mood="Bright and clear.",
        constraints="Keep everything centered.",
        weather_integration="On a large sign.",
    )
    forecast = {
        "weather_code": 0,
        "weather_desc": "Clear sky",
        "weather_icon": "sun",
        "weather_icon_desc": "a bright yellow sun with rays, no clouds",
        "high_f": 70.0,
        "low_f": 50.0,
        "precip_chance": 0,
        "wind_mph": 5.0,
        "sunrise": "2026-04-29T06:00",
        "sunset": "2026-04-29T20:00",
        "timezone": "America/Chicago",
    }

    prompt = _build_prompt(selection, scene, forecast, reference_count=1)

    assert "800x480" in prompt
    assert "six-color e-ink" in prompt
    assert "Avoid tiny text" in prompt
    assert "avoid gradients and fine texture" in prompt
