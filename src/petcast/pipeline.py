"""Orchestrates the full petcast pipeline."""

import json
import os
import time
from datetime import datetime
from pathlib import Path
from zoneinfo import ZoneInfo

from PIL import Image

from petcast.celebrations import (
    active_celebrations,
    apply_celebration_pet_overrides,
    celebration_metadata,
    record_celebration_history,
)
from petcast.config import load_config
from petcast.select import load_history, record_selection, select
from petcast.weather import fetch_forecast
from petcast.scene import generate_scene
from petcast.generate import generate_image, reference_photo_paths
from petcast.dither import dither_for_display


def _step(name: str) -> float:
    """Log a step and return the current time."""
    print(f"[{time.strftime('%H:%M:%S')}] {name}")
    return time.time()


def run(root: Path, debug: bool = False, battery_pct: float | None = None, force_style: str | None = None) -> Path:
    """Run the full pipeline and return the path to latest.png."""
    pipeline_start = time.time()
    config = load_config(root)

    config.output.debug_dir.mkdir(parents=True, exist_ok=True)
    config.output.latest.parent.mkdir(parents=True, exist_ok=True)

    # Step 1: Select
    t = _step("Selecting pets and style...")
    selection = select(config, root)
    if force_style:
        # Find the style that matches the substring
        for s in config.styles:
            if force_style.lower() in s.lower():
                selection = type(selection)(pets=selection.pets, photo=selection.photo, style=s)
                break
    print(f"  ({time.time() - t:.1f}s)")

    # Step 2: Weather
    t = _step("Fetching weather...")
    forecast = fetch_forecast(config)
    print(f"  {forecast['weather_desc']}, {forecast['high_f']:.0f}°/{forecast['low_f']:.0f}°F")
    print(f"  ({time.time() - t:.1f}s)")

    tz = ZoneInfo(forecast["timezone"])
    now = datetime.now(tz)
    celebrations = active_celebrations(config, now.date(), root=root)
    if celebrations:
        selection = apply_celebration_pet_overrides(config, selection, celebrations)
        print("  Celebration: " + "; ".join(c.message for c in celebrations))
    ref_photos = reference_photo_paths(config, selection, root)
    print(f"  Pets: {', '.join(p.name for p in selection.pets)}")
    print(f"  Photo: {selection.photo or '(no reference photo)'}")
    if ref_photos:
        print("  Reference photos: " + ", ".join(path.name for path in ref_photos))
    else:
        print("  Reference photos: (none)")
    print(f"  Style: {selection.style}")

    # Step 3: Scene description
    t = _step("Generating scene description...")
    history = load_history(root)
    scene = generate_scene(
        config,
        selection,
        forecast,
        history,
        battery_pct=battery_pct,
        celebrations=celebrations,
    )
    print(f"  Activity: {scene.activity}")
    print(f"  ({time.time() - t:.1f}s)")

    # Step 4: Image generation (with moderation retry — pick a new style if blocked)
    t = _step("Generating image...")
    raw_image = None
    attempted_styles = {selection.style}
    for attempt in range(3):
        try:
            raw_image = generate_image(
                config,
                selection,
                scene,
                forecast,
                root,
                battery_pct=battery_pct,
                celebrations=celebrations,
            )
            break
        except Exception as e:
            if not _is_moderation_error(e):
                raise
            print(f"  Moderation blocked on attempt {attempt + 1}; re-selecting style and retrying")
            alt_styles = [s for s in config.styles if s not in attempted_styles]
            if not alt_styles:
                raise
            selection = type(selection)(pets=selection.pets, photo=selection.photo, style=alt_styles[0])
            attempted_styles.add(selection.style)
            print(f"  New style: {selection.style[:80]}...")
            scene = generate_scene(
                config,
                selection,
                forecast,
                history,
                battery_pct=battery_pct,
                celebrations=celebrations,
            )
            print(f"  New activity: {scene.activity}")
    if raw_image is None:
        raise RuntimeError("All moderation retries exhausted")
    print(f"  Size: {raw_image.size[0]}x{raw_image.size[1]}")
    print(f"  ({time.time() - t:.1f}s)")
    if debug:
        _save_debug(raw_image, config.output.debug_dir, "01_raw_generated")

    # Step 5: Dither
    t = _step("Dithering for Spectra 6...")
    final = dither_for_display(raw_image, config)
    print(f"  ({time.time() - t:.1f}s)")
    if debug:
        _save_debug(final, config.output.debug_dir, "02_dithered")

    # Record history
    record_selection(root, selection, scene_activity=scene.activity)

    # Step 6: Save
    t = _step("Saving outputs...")
    archive_dir = config.output.archive_dir / now.strftime("%Y/%m/%d")
    archive_dir.mkdir(parents=True, exist_ok=True)
    archive_path = archive_dir / f"petcast_{now.strftime('%H%M%S')}.png"
    final.save(archive_path, "PNG")

    # Also save raw + dithered + metadata flat-named by date for easy sharing
    # (Google Photos sync, etc). Later runs in the same day overwrite.
    config.output.daily_dir.mkdir(parents=True, exist_ok=True)
    date_stem = now.strftime("%Y-%m-%d")
    raw_daily = config.output.daily_dir / f"{date_stem}.png"
    eink_daily = config.output.daily_dir / f"{date_stem}_eink.png"
    _save_rgb(raw_image, raw_daily)
    final.save(eink_daily, "PNG")

    latest = config.output.latest
    latest.unlink(missing_ok=True)
    os.symlink(archive_path.resolve(), latest)

    latest_raw = config.output.latest_raw
    latest_raw.unlink(missing_ok=True)
    os.symlink(raw_daily.resolve(), latest_raw)

    metadata = {
        "generated_at": now.isoformat(),
        "pets": [p.name for p in selection.pets],
        "photo": selection.photo,
        "reference_photos": [path.name for path in ref_photos],
        "style": selection.style,
        "battery_pct": battery_pct,
        "celebrations": celebration_metadata(celebrations),
        "weather": dict(forecast),
        "scene": {
            "activity": scene.activity,
            "foreground": scene.foreground,
            "background": scene.background,
            "mood": scene.mood,
            "weather_integration": scene.weather_integration,
        },
    }

    archive_meta = archive_path.with_suffix(".json")
    with open(archive_meta, "w") as f:
        json.dump(metadata, f, indent=2)

    daily_meta = config.output.daily_dir / f"{date_stem}.json"
    with open(daily_meta, "w") as f:
        json.dump(metadata, f, indent=2)

    record_celebration_history(
        root,
        celebrations,
        generated_at=now,
        scene_activity=scene.activity,
        style=selection.style,
        pet_names=[p.name for p in selection.pets],
        limit_per_event=config.celebrations.history_limit_per_event,
    )

    meta_latest = config.output.metadata
    meta_latest.unlink(missing_ok=True)
    os.symlink(archive_meta.resolve(), meta_latest)
    print(f"  ({time.time() - t:.1f}s)")

    total = time.time() - pipeline_start
    _step(f"Done! Total: {total:.1f}s — saved to {archive_path}")
    return config.output.latest


def _is_moderation_error(exc: Exception) -> bool:
    """Detect OpenAI safety/moderation rejections so we can retry with a different style."""
    msg = str(exc).lower()
    return "moderation_blocked" in msg or "safety system" in msg


def _save_rgb(img: Image.Image, path: Path) -> None:
    """Save as PNG, flattening RGBA onto white."""
    out = img
    if out.mode == "RGBA":
        bg = Image.new("RGB", out.size, (255, 255, 255))
        bg.paste(out, mask=out.split()[3])
        out = bg
    out.save(path, "PNG")


def _save_debug(img: Image.Image, debug_dir: Path, name: str) -> None:
    path = debug_dir / f"{name}.png"
    _save_rgb(img, path)
    print(f"  Debug: {path}")
