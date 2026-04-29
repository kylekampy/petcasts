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
from petcast.generate import reference_photo_paths
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
