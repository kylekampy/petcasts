"""Special-day prompt context and private celebration history."""

import calendar
import json
import re
from dataclasses import dataclass, field
from datetime import date, datetime
from pathlib import Path
from typing import Any

from petcast.config import CelebrationEventConfig, Config, Pet


@dataclass
class ActiveCelebration:
    id: str
    name: str
    kind: str
    message: str
    prompt: str
    years: int | None
    ordinal: str | None
    pets: str | list[str]
    group: str | None
    photo: str | None
    priority: int
    previous_activities: list[str] = field(default_factory=list)


def active_celebrations(
    config: Config,
    today: date,
    root: Path | None = None,
) -> list[ActiveCelebration]:
    """Return all celebrations that should influence today's generation."""
    if not config.celebrations.enabled:
        return []

    history = load_celebration_history(root) if root is not None else {}
    events = []
    if config.celebrations.include_builtin_holidays:
        events.extend(_BUILTIN_HOLIDAYS)
    events.extend(config.celebrations.events)

    active = []
    for event in events:
        celebration = _activate(event, today, history)
        if celebration is not None:
            active.append(celebration)

    return sorted(active, key=lambda c: c.priority, reverse=True)


def apply_celebration_pet_overrides(
    config: Config,
    selection,
    celebrations: list[ActiveCelebration],
):
    """Use the highest-priority celebration pet request, if one exists."""
    for celebration in celebrations:
        pets = _resolve_pets(config, celebration)
        if pets is None:
            continue
        photo = _resolve_photo(config, pets, celebration.photo)
        return type(selection)(pets=pets, photo=photo, style=selection.style)
    return selection


def scene_prompt_block(celebrations: list[ActiveCelebration]) -> str:
    if not celebrations:
        return ""

    lines = [
        "SPECIAL DAY:",
        "Today has one or more celebration themes. Make the celebration feel central,",
        "but still keep the daily weather forecast and real season integrated.",
        "Invent a fresh scene idea rather than using a generic party setup.",
    ]

    for celebration in celebrations:
        lines.append(f"- {celebration.name}: {celebration.message}")
        if celebration.prompt:
            lines.append(f"  Guidance: {celebration.prompt}")
        if celebration.previous_activities:
            lines.append("  Prior ideas for this same event; avoid repeating them:")
            for activity in celebration.previous_activities[:5]:
                lines.append(f"  - {activity}")

    return "\n".join(lines)


def image_prompt_block(celebrations: list[ActiveCelebration]) -> str:
    if not celebrations:
        return ""

    lines = [
        "SPECIAL CELEBRATION INFO: Incorporate today's celebration into the artwork.",
        "Readable celebration text is allowed and should be integrated naturally into",
        "the same object/setting as the weather info when possible.",
    ]
    for celebration in celebrations:
        lines.append(f"- Include the readable text: {celebration.message!r}")
        if celebration.prompt:
            lines.append(f"  Visual guidance: {celebration.prompt}")
    return "\n".join(lines)


def celebration_metadata(celebrations: list[ActiveCelebration]) -> list[dict]:
    return [
        {
            "id": celebration.id,
            "name": celebration.name,
            "kind": celebration.kind,
            "message": celebration.message,
            "years": celebration.years,
            "ordinal": celebration.ordinal,
        }
        for celebration in celebrations
    ]


def load_celebration_history(root: Path | None) -> dict[str, Any]:
    if root is None:
        return {}
    path = _history_path(root)
    if not path.exists():
        return {}
    with open(path) as f:
        data = json.load(f)
    if not isinstance(data, dict):
        return {}
    return data


def record_celebration_history(
    root: Path,
    celebrations: list[ActiveCelebration],
    *,
    generated_at: datetime,
    scene_activity: str,
    style: str,
    pet_names: list[str],
    limit_per_event: int,
) -> None:
    if not celebrations:
        return

    path = _history_path(root)
    path.parent.mkdir(parents=True, exist_ok=True)
    data = load_celebration_history(root)
    events = data.setdefault("events", {})
    event_date = generated_at.date().isoformat()

    for celebration in celebrations:
        entries = list(events.get(celebration.id, []))
        entries = [entry for entry in entries if entry.get("date") != event_date]
        entries.append(
            {
                "date": event_date,
                "year": generated_at.year,
                "message": celebration.message,
                "activity": scene_activity,
                "style": style,
                "pets": pet_names,
            }
        )
        events[celebration.id] = entries[-limit_per_event:]

    with open(path, "w") as f:
        json.dump(data, f, indent=2)


def _activate(
    event: CelebrationEventConfig,
    today: date,
    history: dict[str, Any],
) -> ActiveCelebration | None:
    if not event.enabled:
        return None

    event_date, start_year = _scheduled_date(event, today.year)
    if event_date != today:
        return None

    start_year = event.start_year if event.start_year is not None else start_year
    years = today.year - start_year if start_year is not None else None
    ord_text = ordinal(years) if years is not None else None
    message = _message(event, today, years, ord_text)
    previous = _previous_activities(history, event.id)

    return ActiveCelebration(
        id=event.id,
        name=event.name,
        kind=event.kind,
        message=message,
        prompt=event.prompt,
        years=years,
        ordinal=ord_text,
        pets=event.pets,
        group=event.group,
        photo=event.photo,
        priority=event.priority,
        previous_activities=previous,
    )


def _scheduled_date(event: CelebrationEventConfig, year: int) -> tuple[date, int | None]:
    if event.rule:
        return _rule_date(event, year), event.start_year
    if event.month is not None and event.day is not None:
        return date(year, event.month, event.day), event.start_year
    if event.date:
        month, day, start_year = _parse_date(event.date)
        return date(year, month, day), start_year
    raise ValueError(f"Celebration event {event.id!r} needs date/month-day/rule")


def _rule_date(event: CelebrationEventConfig, year: int) -> date:
    rule = event.rule.lower().replace("-", "_")
    if rule != "nth_weekday":
        raise ValueError(f"Unsupported celebration date rule: {event.rule!r}")
    if event.month is None or event.weekday is None or event.nth is None:
        raise ValueError(f"nth_weekday event {event.id!r} needs month, weekday, and nth")

    weekday = _weekday_index(event.weekday)
    weeks = calendar.monthcalendar(year, event.month)
    days = [week[weekday] for week in weeks if week[weekday] != 0]
    day = days[event.nth - 1] if event.nth > 0 else days[event.nth]
    return date(year, event.month, day)


def _parse_date(value: str) -> tuple[int, int, int | None]:
    text = re.sub(r"(\d)(st|nd|rd|th)\b", r"\1", value.strip(), flags=re.I)
    formats = [
        ("%Y-%m-%d", True),
        ("%m-%d", False),
        ("%m/%d/%Y", True),
        ("%m/%d/%y", True),
        ("%B %d, %Y", True),
        ("%B %d, %y", True),
        ("%b %d, %Y", True),
        ("%b %d, %y", True),
        ("%B %d", False),
        ("%b %d", False),
    ]
    for fmt, has_year in formats:
        try:
            parse_text = text if has_year else f"{text} 2000"
            parse_fmt = fmt if has_year else f"{fmt} %Y"
            parsed = datetime.strptime(parse_text, parse_fmt)
            return parsed.month, parsed.day, parsed.year if has_year else None
        except ValueError:
            continue
    raise ValueError(f"Unsupported celebration date: {value!r}")


def _message(
    event: CelebrationEventConfig,
    today: date,
    years: int | None,
    ord_text: str | None,
) -> str:
    template = event.message_template or _default_message_template(event, years)
    values = {
        "name": event.name,
        "event": event.name,
        "year": today.year,
        "years": "" if years is None else years,
        "ordinal": "" if ord_text is None else ord_text,
    }
    return template.format(**values)


def _default_message_template(event: CelebrationEventConfig, years: int | None) -> str:
    kind = event.kind.lower()
    if kind == "birthday" and years is not None:
        return "Happy {ordinal} Birthday, {name}"
    if kind == "anniversary" and years is not None:
        return "Happy {ordinal} Anniversary"
    return "{name}"


def ordinal(n: int) -> str:
    if 10 <= n % 100 <= 20:
        suffix = "th"
    else:
        suffix = {1: "st", 2: "nd", 3: "rd"}.get(n % 10, "th")
    return f"{n}{suffix}"


def _previous_activities(history: dict[str, Any], event_id: str) -> list[str]:
    entries = history.get("events", {}).get(event_id, [])
    activities = [
        entry.get("activity")
        for entry in reversed(entries)
        if isinstance(entry, dict) and entry.get("activity")
    ]
    return activities


def _resolve_pets(config: Config, celebration: ActiveCelebration) -> list[Pet] | None:
    spec = celebration.pets
    if celebration.group:
        return _pets_from_group(config, celebration.group)
    if isinstance(spec, list):
        return _pets_by_name(config, spec)

    spec_text = str(spec).strip()
    spec_lower = spec_text.lower()
    if spec_lower in {"", "selected", "default"}:
        return None
    if spec_lower == "all":
        return config.pets
    if spec_lower.startswith("group:"):
        return _pets_from_group(config, spec_text.split(":", 1)[1].strip())
    if spec_lower in {group.name.lower(): group for group in config.groups}:
        return _pets_from_group(config, spec_text)
    return _pets_by_name(config, [spec_text])


def _pets_by_name(config: Config, names: list[str]) -> list[Pet]:
    by_name = {pet.name.lower(): pet for pet in config.pets}
    missing = [name for name in names if name.lower() not in by_name]
    if missing:
        raise ValueError(f"Unknown pet(s) in celebration config: {', '.join(missing)}")
    return [by_name[name.lower()] for name in names]


def _pets_from_group(config: Config, group_name: str) -> list[Pet]:
    groups = {group.name.lower(): group for group in config.groups}
    group = groups.get(group_name.lower())
    if group is None:
        raise ValueError(f"Unknown pet group in celebration config: {group_name!r}")
    return _pets_by_name(config, group.pet_names)


def _resolve_photo(config: Config, pets: list[Pet], configured_photo: str | None) -> str | None:
    if configured_photo:
        if configured_photo.strip().lower() in {"none", "no_photo", "null"}:
            return None
        return configured_photo

    target = {pet.name for pet in pets}
    photo_to_names: dict[str, set[str]] = {}
    for pet in config.pets:
        for photo in pet.photos:
            photo_to_names.setdefault(photo, set()).add(pet.name)

    candidates = [
        (len(names - target), photo)
        for photo, names in photo_to_names.items()
        if target <= names
    ]
    if not candidates:
        return None
    return sorted(candidates)[0][1]


def _weekday_index(value: str | int) -> int:
    if isinstance(value, int):
        return value
    names = {name.lower(): i for i, name in enumerate(calendar.day_name)}
    abbr = {name.lower(): i for i, name in enumerate(calendar.day_abbr)}
    key = value.strip().lower()
    if key in names:
        return names[key]
    if key in abbr:
        return abbr[key]
    raise ValueError(f"Unknown weekday in celebration config: {value!r}")


def _history_path(root: Path) -> Path:
    return root / "pets" / "state" / "celebrations.json"


_BUILTIN_HOLIDAYS = [
    CelebrationEventConfig(
        id="new-years-day",
        name="New Year's Day",
        kind="holiday",
        month=1,
        day=1,
        message_template="Happy New Year",
        prompt="Use fresh-start imagery: a new calendar page, tiny party hats, confetti, or sunrise optimism.",
    ),
    CelebrationEventConfig(
        id="valentines-day",
        name="Valentine's Day",
        kind="holiday",
        month=2,
        day=14,
        message_template="Happy Valentine's Day",
        prompt="Make it affectionate and sweet without becoming generic: handmade valentines, heart-shaped treats, or cozy gestures.",
    ),
    CelebrationEventConfig(
        id="pi-day",
        name="Pi Day",
        kind="holiday",
        month=3,
        day=14,
        message_template="Happy Pi Day",
        prompt="Work in pie, circles, math doodles, or 3.14 in a clever pet-weather way.",
    ),
    CelebrationEventConfig(
        id="st-patricks-day",
        name="St. Patrick's Day",
        kind="holiday",
        month=3,
        day=17,
        message_template="Happy St. Patrick's Day",
        prompt="Use playful green celebration details, clover motifs, and cozy pub-or-parade energy.",
    ),
    CelebrationEventConfig(
        id="april-fools-day",
        name="April Fools' Day",
        kind="holiday",
        month=4,
        day=1,
        message_template="April Fools' Day",
        prompt="Make the joke gentle and visual: harmless pranks, silly props, swapped signs, or pets pretending to be meteorologists.",
    ),
    CelebrationEventConfig(
        id="earth-day",
        name="Earth Day",
        kind="holiday",
        month=4,
        day=22,
        message_template="Earth Day",
        prompt="Emphasize caring for the local landscape: planting, cleanup, pollinator gardens, or tiny conservation work.",
    ),
    CelebrationEventConfig(
        id="may-the-fourth",
        name="May the Fourth",
        kind="holiday",
        month=5,
        day=4,
        message_template="May the Fourth",
        prompt="Use a playful space-adventure pun with cardboard starships, glowing toy swords, constellations, or heroic pet poses.",
    ),
    CelebrationEventConfig(
        id="cinco-de-mayo",
        name="Cinco de Mayo",
        kind="holiday",
        month=5,
        day=5,
        message_template="Cinco de Mayo",
        prompt="Use bright celebratory details, festive food, papel picado, and music without stereotypes.",
    ),
    CelebrationEventConfig(
        id="mothers-day",
        name="Mother's Day",
        kind="holiday",
        rule="nth_weekday",
        month=5,
        weekday="sunday",
        nth=2,
        message_template="Happy Mother's Day",
        prompt="Make it warm and appreciative: flowers, breakfast trays, handmade cards, or a cozy family portrait.",
    ),
    CelebrationEventConfig(
        id="memorial-day",
        name="Memorial Day",
        kind="holiday",
        rule="nth_weekday",
        month=5,
        weekday="monday",
        nth=-1,
        message_template="Memorial Day",
        prompt="Keep it respectful and summery: flags, porch gatherings, remembrance, and early-summer weather.",
    ),
    CelebrationEventConfig(
        id="fathers-day",
        name="Father's Day",
        kind="holiday",
        rule="nth_weekday",
        month=6,
        weekday="sunday",
        nth=3,
        message_template="Happy Father's Day",
        prompt="Make it affectionate and funny: grilling, tools, handmade cards, lawn chairs, or dad-joke energy.",
    ),
    CelebrationEventConfig(
        id="juneteenth",
        name="Juneteenth",
        kind="holiday",
        month=6,
        day=19,
        message_template="Juneteenth",
        prompt="Use celebratory community details, red foods or banners, and a respectful freedom-day mood.",
    ),
    CelebrationEventConfig(
        id="independence-day",
        name="Independence Day",
        kind="holiday",
        month=7,
        day=4,
        message_template="Happy Fourth of July",
        prompt="Make it festive and summer-specific: picnic details, flags, sparklers, or parade energy.",
    ),
    CelebrationEventConfig(
        id="labor-day",
        name="Labor Day",
        kind="holiday",
        rule="nth_weekday",
        month=9,
        weekday="monday",
        nth=1,
        message_template="Labor Day",
        prompt="Use end-of-summer details: cookout, hammocks, tools set aside, or a well-earned day off.",
    ),
    CelebrationEventConfig(
        id="halloween",
        name="Halloween",
        kind="holiday",
        month=10,
        day=31,
        message_template="Happy Halloween",
        prompt="Make it cute-spooky rather than scary: costumes, pumpkins, candy bowls, moonlit porches, or trick-or-treat details.",
    ),
    CelebrationEventConfig(
        id="veterans-day",
        name="Veterans Day",
        kind="holiday",
        month=11,
        day=11,
        message_template="Veterans Day",
        prompt="Keep it respectful and calm: flags, gratitude cards, and a sincere community mood.",
    ),
    CelebrationEventConfig(
        id="thanksgiving",
        name="Thanksgiving",
        kind="holiday",
        rule="nth_weekday",
        month=11,
        weekday="thursday",
        nth=4,
        message_template="Happy Thanksgiving",
        prompt="Make it cozy and grateful: harvest table, shared meal, thankful notes, or pets helping prepare dinner.",
    ),
    CelebrationEventConfig(
        id="christmas-eve",
        name="Christmas Eve",
        kind="holiday",
        month=12,
        day=24,
        message_template="Christmas Eve",
        prompt="Use quiet anticipation: stockings, warm lights, wrapped packages, cocoa, or nighttime snow if weather fits.",
    ),
    CelebrationEventConfig(
        id="christmas-day",
        name="Christmas Day",
        kind="holiday",
        month=12,
        day=25,
        message_template="Merry Christmas",
        prompt="Make it warm and celebratory: presents, garland, breakfast, cozy lights, or pets opening treats.",
    ),
    CelebrationEventConfig(
        id="new-years-eve",
        name="New Year's Eve",
        kind="holiday",
        month=12,
        day=31,
        message_template="Happy New Year's Eve",
        prompt="Use countdown details, party hats, confetti, clocks, or reflective year-end coziness.",
    ),
]
