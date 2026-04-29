"""Load and validate config.yaml and pets.yaml."""

import os
from dataclasses import dataclass, field
from pathlib import Path

import yaml


@dataclass
class LocationConfig:
    name: str
    latitude: float
    longitude: float


@dataclass
class GeminiConfig:
    image_model: str
    chat_model: str


@dataclass
class OpenAIConfig:
    image_model: str
    quality: str
    size: str
    chat_model: str


@dataclass
class DisplayConfig:
    width: int
    height: int


@dataclass
class OutputConfig:
    latest: Path
    latest_raw: Path
    metadata: Path
    debug_dir: Path
    archive_dir: Path
    daily_dir: Path


@dataclass
class CooldownConfig:
    photo_days: int
    combo_days: int
    style_uses: int


@dataclass
class CelebrationEventConfig:
    id: str
    name: str
    kind: str = "custom"
    date: str | None = None
    month: int | None = None
    day: int | None = None
    rule: str | None = None
    weekday: str | int | None = None
    nth: int | None = None
    start_year: int | None = None
    message_template: str | None = None
    prompt: str = ""
    pets: str | list[str] = "selected"
    group: str | None = None
    photo: str | None = None
    priority: int = 0
    enabled: bool = True


@dataclass
class CelebrationConfig:
    enabled: bool = True
    include_builtin_holidays: bool = True
    history_limit_per_event: int = 12
    events: list[CelebrationEventConfig] = field(default_factory=list)


@dataclass
class Pet:
    name: str
    description: str
    photos: list[str]


@dataclass
class PetGroup:
    name: str
    pet_names: list[str]


@dataclass
class Config:
    location: LocationConfig
    styles: list[str]
    image_provider: str
    scene_provider: str
    gemini: GeminiConfig
    openai: OpenAIConfig
    display: DisplayConfig
    output: OutputConfig
    cooldowns: CooldownConfig
    celebrations: CelebrationConfig
    pets: list[Pet] = field(default_factory=list)
    groups: list[PetGroup] = field(default_factory=list)


def load_config(root: Path) -> Config:
    """Load config.yaml and pets.yaml from project root."""
    raw = _load_yaml(root / "config.yaml")
    for overlay in _private_config_paths(root):
        if overlay.exists():
            raw = _merge_config(raw, _load_yaml(overlay))

    with open(root / "pets" / "meta" / "pets.yaml") as f:
        pets_raw = yaml.safe_load(f)

    loc = raw["location"]
    gem = raw["gemini"]
    oai = raw.get("openai", {})
    disp = raw["display"]
    out = raw["output"]
    cd = raw["cooldowns"]
    celebrations = raw.get("celebrations", {})

    return Config(
        location=LocationConfig(
            name=loc["name"],
            latitude=loc["latitude"],
            longitude=loc["longitude"],
        ),
        styles=raw["styles"],
        image_provider=raw.get("image_provider", "gemini"),
        scene_provider=raw.get("scene_provider", "gemini"),
        gemini=GeminiConfig(
            image_model=gem["image_model"],
            chat_model=gem["chat_model"],
        ),
        openai=OpenAIConfig(
            image_model=oai.get("image_model", "gpt-image-2"),
            quality=oai.get("quality", "medium"),
            size=oai.get("size", "1536x1024"),
            chat_model=oai.get("chat_model", "gpt-5.4-mini"),
        ),
        display=DisplayConfig(width=disp["width"], height=disp["height"]),
        output=OutputConfig(
            latest=root / out["latest"],
            latest_raw=root / out.get("latest_raw", "output/latest_raw.png"),
            metadata=root / out["metadata"],
            debug_dir=root / out["debug_dir"],
            archive_dir=root / out["archive_dir"],
            daily_dir=root / out.get("daily_dir", "output/daily"),
        ),
        cooldowns=CooldownConfig(
            photo_days=cd["photo_days"],
            combo_days=cd["combo_days"],
            style_uses=cd["style_uses"],
        ),
        celebrations=CelebrationConfig(
            enabled=celebrations.get("enabled", True),
            include_builtin_holidays=celebrations.get("include_builtin_holidays", True),
            history_limit_per_event=celebrations.get("history_limit_per_event", 12),
            events=[
                _parse_celebration_event(event)
                for event in celebrations.get("events", [])
            ],
        ),
        pets=[
            Pet(name=p["name"], description=p["description"], photos=p.get("photos") or [])
            for p in pets_raw["pets"]
        ],
        groups=[
            PetGroup(name=g["name"], pet_names=g["pets"])
            for g in pets_raw.get("groups", [])
        ],
    )


def _load_yaml(path: Path) -> dict:
    with open(path) as f:
        return yaml.safe_load(f) or {}


def _private_config_paths(root: Path) -> list[Path]:
    paths = [root / "config.local.yaml"]
    extra = os.environ.get("PETCAST_PRIVATE_CONFIG")
    if extra:
        for item in extra.split(os.pathsep):
            if not item:
                continue
            path = Path(item)
            paths.append(path if path.is_absolute() else root / path)
    return paths


def _merge_config(base: dict, override: dict, path: tuple[str, ...] = ()) -> dict:
    """Merge a private config overlay into the checked-in base config.

    `celebrations.events` appends so public holidays/examples and private dates can
    coexist. Other lists, such as styles, are intentionally replaced by overlays.
    """
    merged = dict(base)
    for key, value in override.items():
        if (
            path == ("celebrations",)
            and key == "events"
            and isinstance(merged.get(key), list)
            and isinstance(value, list)
        ):
            merged[key] = [*merged[key], *value]
        elif isinstance(merged.get(key), dict) and isinstance(value, dict):
            merged[key] = _merge_config(merged[key], value, (*path, key))
        else:
            merged[key] = value
    return merged


def _parse_celebration_event(raw: dict) -> CelebrationEventConfig:
    raw_date = raw.get("date")
    if hasattr(raw_date, "isoformat"):
        raw_date = raw_date.isoformat()
    elif raw_date is not None:
        raw_date = str(raw_date)

    return CelebrationEventConfig(
        id=raw["id"],
        name=raw["name"],
        kind=raw.get("kind", "custom"),
        date=raw_date,
        month=raw.get("month"),
        day=raw.get("day"),
        rule=raw.get("rule"),
        weekday=raw.get("weekday"),
        nth=raw.get("nth"),
        start_year=raw.get("start_year"),
        message_template=raw.get("message_template"),
        prompt=raw.get("prompt", ""),
        pets=raw.get("pets", "selected"),
        group=raw.get("group"),
        photo=raw.get("photo"),
        priority=raw.get("priority", 0),
        enabled=raw.get("enabled", True),
    )
