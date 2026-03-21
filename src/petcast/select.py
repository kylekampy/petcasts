"""Pet/photo picker with cooldown logic."""

import json
import random
from dataclasses import dataclass
from datetime import datetime, timedelta
from pathlib import Path

from petcast.config import Config, Pet


@dataclass
class Selection:
    pets: list[Pet]
    photo: str  # reference photo filename — pets are derived from this
    style: str


def load_history(root: Path) -> list[dict]:
    path = root / "pets" / "state" / "history.json"
    if not path.exists():
        return []
    with open(path) as f:
        return json.load(f)


def save_history(root: Path, history: list[dict]) -> None:
    path = root / "pets" / "state" / "history.json"
    path.parent.mkdir(parents=True, exist_ok=True)
    with open(path, "w") as f:
        json.dump(history, f, indent=2)


def select(config: Config, root: Path) -> Selection:
    """Pick a random photo, find which pets are in it, pick a style."""
    history = load_history(root)
    now = datetime.now()

    # Build reverse index: photo -> list of pets that list it
    photo_to_pets: dict[str, list[Pet]] = {}
    for pet in config.pets:
        for photo in pet.photos:
            photo_to_pets.setdefault(photo, []).append(pet)

    all_photos = list(photo_to_pets.keys())

    # Gather recently used items
    recent_photos: set[str] = set()
    recent_styles: dict[str, int] = {}

    for entry in history:
        entry_date = datetime.fromisoformat(entry["date"])
        age = now - entry_date

        if age < timedelta(days=config.cooldowns.photo_days):
            if "photo" in entry:
                recent_photos.add(entry["photo"])
            for photo in entry.get("photos", []):
                recent_photos.add(photo)

        if age < timedelta(days=config.cooldowns.combo_days):
            style = entry.get("style", "")
            if style:
                recent_styles[style] = recent_styles.get(style, 0) + 1

    # Pick a photo not recently used
    available = [p for p in all_photos if p not in recent_photos]
    if not available:
        available = all_photos
    chosen_photo = random.choice(available)

    # Pets are those who list this photo
    chosen_pets = photo_to_pets[chosen_photo]

    # Pick style, preferring less-used ones
    style_weights = []
    for style in config.styles:
        uses = recent_styles.get(style, 0)
        weight = max(1.0, config.cooldowns.style_uses - uses)
        style_weights.append(weight)

    chosen_style = random.choices(config.styles, weights=style_weights, k=1)[0]

    return Selection(pets=chosen_pets, photo=chosen_photo, style=chosen_style)


def record_selection(
    root: Path, selection: Selection, scene_activity: str | None = None
) -> None:
    """Append a selection to history."""
    history = load_history(root)
    entry: dict = {
        "date": datetime.now().isoformat(),
        "pet_names": [p.name for p in selection.pets],
        "photo": selection.photo,
        "style": selection.style,
    }
    if scene_activity:
        entry["scene"] = {"activity": scene_activity}
    history.append(entry)
    # Keep last 90 days
    cutoff = datetime.now() - timedelta(days=90)
    history = [
        e for e in history if datetime.fromisoformat(e["date"]) > cutoff
    ]
    save_history(root, history)
