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
    photos: list[str]  # one photo filename per pet
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
    """Pick pets, photos, and a style, suppressing recent usage."""
    history = load_history(root)
    now = datetime.now()

    # Gather recently used items
    recent_photos: set[str] = set()
    recent_combos: set[str] = set()
    recent_styles: dict[str, int] = {}

    for entry in history:
        entry_date = datetime.fromisoformat(entry["date"])
        age = now - entry_date

        if age < timedelta(days=config.cooldowns.photo_days):
            for photo in entry.get("photos", []):
                recent_photos.add(photo)

        if age < timedelta(days=config.cooldowns.combo_days):
            combo_key = _combo_key(entry.get("pet_names", []))
            if combo_key:
                recent_combos.add(combo_key)

        # Count style uses within the cooldown window
        if age < timedelta(days=config.cooldowns.combo_days):
            style = entry.get("style", "")
            if style:
                recent_styles[style] = recent_styles.get(style, 0) + 1

    # Pick 1-2 pets (if we have multiple)
    num_pets = min(random.choice([1, 1, 2]), len(config.pets))
    pet_pool = list(config.pets)
    random.shuffle(pet_pool)

    # Try to avoid recent combos
    chosen_pets: list[Pet] = []
    for pet in pet_pool:
        if len(chosen_pets) >= num_pets:
            break
        trial = chosen_pets + [pet]
        combo = _combo_key([p.name for p in trial])
        if combo not in recent_combos or len(chosen_pets) == 0:
            chosen_pets.append(pet)

    # If we couldn't fill, just take whatever
    if len(chosen_pets) < num_pets:
        for pet in pet_pool:
            if pet not in chosen_pets:
                chosen_pets.append(pet)
            if len(chosen_pets) >= num_pets:
                break

    # Pick one photo per pet, avoiding recently used
    chosen_photos: list[str] = []
    for pet in chosen_pets:
        available = [p for p in pet.photos if p not in recent_photos]
        if not available:
            available = pet.photos  # all on cooldown, reset
        chosen_photos.append(random.choice(available))

    # Pick style, preferring less-used ones
    style_weights = []
    for style in config.styles:
        uses = recent_styles.get(style, 0)
        weight = max(1.0, config.cooldowns.style_uses - uses)
        style_weights.append(weight)

    chosen_style = random.choices(config.styles, weights=style_weights, k=1)[0]

    return Selection(pets=chosen_pets, photos=chosen_photos, style=chosen_style)


def record_selection(
    root: Path, selection: Selection, scene_activity: str | None = None
) -> None:
    """Append a selection to history."""
    history = load_history(root)
    entry: dict = {
        "date": datetime.now().isoformat(),
        "pet_names": [p.name for p in selection.pets],
        "photos": selection.photos,
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


def _combo_key(names: list[str]) -> str:
    return "|".join(sorted(names))
