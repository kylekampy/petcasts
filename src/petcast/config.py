"""Load and validate config.yaml and pets.yaml."""

from dataclasses import dataclass, field
from pathlib import Path

import yaml


@dataclass
class LocationConfig:
    name: str
    latitude: float
    longitude: float


@dataclass
class OpenAIConfig:
    model: str
    chat_model: str
    size: str


@dataclass
class DisplayConfig:
    width: int
    height: int


@dataclass
class OutputConfig:
    latest: Path
    metadata: Path
    debug_dir: Path
    archive_dir: Path


@dataclass
class CooldownConfig:
    photo_days: int
    combo_days: int
    style_uses: int


@dataclass
class Pet:
    name: str
    description: str
    photos: list[str]


@dataclass
class Config:
    location: LocationConfig
    styles: list[str]
    openai: OpenAIConfig
    display: DisplayConfig
    output: OutputConfig
    cooldowns: CooldownConfig
    pets: list[Pet] = field(default_factory=list)


def load_config(root: Path) -> Config:
    """Load config.yaml and pets.yaml from project root."""
    with open(root / "config.yaml") as f:
        raw = yaml.safe_load(f)

    with open(root / "pets" / "meta" / "pets.yaml") as f:
        pets_raw = yaml.safe_load(f)

    loc = raw["location"]
    ai = raw["openai"]
    disp = raw["display"]
    out = raw["output"]
    cd = raw["cooldowns"]

    return Config(
        location=LocationConfig(
            name=loc["name"],
            latitude=loc["latitude"],
            longitude=loc["longitude"],
        ),
        styles=raw["styles"],
        openai=OpenAIConfig(
            model=ai["model"],
            chat_model=ai["chat_model"],
            size=ai["size"],
        ),
        display=DisplayConfig(width=disp["width"], height=disp["height"]),
        output=OutputConfig(
            latest=root / out["latest"],
            metadata=root / out["metadata"],
            debug_dir=root / out["debug_dir"],
            archive_dir=root / out["archive_dir"],
        ),
        cooldowns=CooldownConfig(
            photo_days=cd["photo_days"],
            combo_days=cd["combo_days"],
            style_uses=cd["style_uses"],
        ),
        pets=[
            Pet(name=p["name"], description=p["description"], photos=p.get("photos") or [])
            for p in pets_raw["pets"]
        ],
    )
