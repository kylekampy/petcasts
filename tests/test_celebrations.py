from datetime import date
from pathlib import Path

from petcast.celebrations import active_celebrations, apply_celebration_pet_overrides
from petcast.config import (
    CelebrationConfig,
    CelebrationEventConfig,
    Config,
    CooldownConfig,
    DisplayConfig,
    GeminiConfig,
    LocationConfig,
    OpenAIConfig,
    OutputConfig,
    Pet,
    PetGroup,
    load_config,
)
from petcast.select import Selection


def _config(
    events=None,
    include_builtin_holidays=False,
    pets=None,
    groups=None,
):
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
        celebrations=CelebrationConfig(
            include_builtin_holidays=include_builtin_holidays,
            events=events or [],
        ),
        pets=pets or [],
        groups=groups or [],
    )


def test_birthday_message_uses_age_ordinal():
    config = _config([
        CelebrationEventConfig(
            id="birthday",
            name="Person",
            kind="birthday",
            date="2000-04-24",
        )
    ])

    celebrations = active_celebrations(config, date(2036, 4, 24))

    assert len(celebrations) == 1
    assert celebrations[0].years == 36
    assert celebrations[0].ordinal == "36th"
    assert celebrations[0].message == "Happy 36th Birthday, Person"


def test_two_digit_year_and_ordinal_suffix_are_accepted():
    config = _config([
        CelebrationEventConfig(
            id="birthday",
            name="Casey",
            kind="birthday",
            date="Nov 1st, 00",
        )
    ])

    celebrations = active_celebrations(config, date(2026, 11, 1))

    assert celebrations[0].years == 26
    assert celebrations[0].message == "Happy 26th Birthday, Casey"


def test_dynamic_builtin_holiday_matches_thanksgiving():
    config = _config(include_builtin_holidays=True)

    celebrations = active_celebrations(config, date(2026, 11, 26))

    assert [c.id for c in celebrations] == ["thanksgiving"]
    assert celebrations[0].message == "Happy Thanksgiving"


def test_all_pet_override_can_drop_reference_photo_when_no_shared_photo():
    alice = Pet("Alice", "gray cat", ["alice.png"])
    bob = Pet("Bob", "brown dog", ["bob.png"])
    config = _config(
        events=[
            CelebrationEventConfig(
                id="anniversary",
                name="Anniversary",
                kind="anniversary",
                date="2001-06-15",
                pets="all",
            )
        ],
        pets=[alice, bob],
    )
    celebrations = active_celebrations(config, date(2041, 6, 15))
    selection = Selection(pets=[alice], photo="alice.png", style="style")

    result = apply_celebration_pet_overrides(config, selection, celebrations)

    assert [pet.name for pet in result.pets] == ["Alice", "Bob"]
    assert result.photo is None


def test_group_pet_override_uses_shared_reference_photo():
    alice = Pet("Alice", "gray cat", ["alice.png", "both.png"])
    bob = Pet("Bob", "brown dog", ["both.png"])
    config = _config(
        events=[
            CelebrationEventConfig(
                id="group-day",
                name="Group Day",
                date="05-03",
                group="the pair",
            )
        ],
        pets=[alice, bob],
        groups=[PetGroup("the pair", ["Alice", "Bob"])],
    )
    celebrations = active_celebrations(config, date(2026, 5, 3))
    selection = Selection(pets=[alice], photo="alice.png", style="style")

    result = apply_celebration_pet_overrides(config, selection, celebrations)

    assert [pet.name for pet in result.pets] == ["Alice", "Bob"]
    assert result.photo == "both.png"


def test_local_config_appends_private_events(tmp_path):
    (tmp_path / "pets" / "meta").mkdir(parents=True)
    (tmp_path / "config.yaml").write_text(
        """
location:
  name: Test
  latitude: 0
  longitude: 0
styles:
  - style
image_provider: openai
scene_provider: openai
gemini:
  image_model: gemini-image
  chat_model: gemini-chat
openai:
  image_model: openai-image
  quality: medium
  size: 1536x1024
  chat_model: openai-chat
display:
  width: 800
  height: 480
output:
  latest: output/latest.png
  metadata: output/latest.json
  debug_dir: output/debug
  archive_dir: output/archive
cooldowns:
  photo_days: 7
  combo_days: 14
  style_uses: 7
celebrations:
  events:
    - id: public
      name: Public
      date: '05-01'
""".strip()
    )
    (tmp_path / "config.local.yaml").write_text(
        """
celebrations:
  events:
    - id: private
      name: Private
      date: '05-02'
""".strip()
    )
    (tmp_path / "pets" / "meta" / "pets.yaml").write_text("pets: []\n")

    config = load_config(tmp_path)

    assert [event.id for event in config.celebrations.events] == ["public", "private"]
