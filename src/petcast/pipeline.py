"""Orchestrates the full petcast pipeline."""

import json
import os
from datetime import datetime
from pathlib import Path
from zoneinfo import ZoneInfo

from PIL import Image

from petcast.config import load_config
from petcast.select import load_history, record_selection, select
from petcast.weather import fetch_forecast
from petcast.scene import generate_scene
from petcast.generate import generate_image
from petcast.dither import dither_for_display


def run(root: Path, debug: bool = False) -> Path:
    """Run the full pipeline and return the path to latest.png."""
    config = load_config(root)

    # Ensure output dirs exist
    config.output.debug_dir.mkdir(parents=True, exist_ok=True)
    config.output.latest.parent.mkdir(parents=True, exist_ok=True)

    # Step 1: Select pets, photo, style
    print("Selecting pets and style...")
    selection = select(config, root)
    print(f"  Pets: {', '.join(p.name for p in selection.pets)}")
    print(f"  Photo: {selection.photo}")
    print(f"  Style: {selection.style}")

    # Step 2: Fetch weather
    print("Fetching weather...")
    forecast = fetch_forecast(config)
    print(f"  {forecast['weather_desc']}, {forecast['high_f']:.0f}°/{forecast['low_f']:.0f}°F")

    # Step 3: Generate scene description
    print("Generating scene description...")
    history = load_history(root)
    scene = generate_scene(config, selection, forecast, history)
    print(f"  Activity: {scene.activity}")
    print(f"  Overlay: {scene.overlay_position}")

    # Step 4: Generate image
    print("Generating image...")
    raw_image = generate_image(config, selection, scene, forecast, root)
    if debug:
        _save_debug(raw_image, config.output.debug_dir, "01_raw_generated")

    # Step 5: Dither for display
    print("Dithering for Spectra 6...")
    final = dither_for_display(raw_image, config)
    if debug:
        _save_debug(final, config.output.debug_dir, "02_dithered")

    # Record selection to history (before saves so cooldowns work even if save fails)
    record_selection(root, selection, scene_activity=scene.activity)

    # Step 7: Save outputs
    print("Saving outputs...")

    # Use the location's timezone for timestamps and file paths
    tz = ZoneInfo(forecast["timezone"])
    now = datetime.now(tz)
    archive_dir = config.output.archive_dir / now.strftime("%Y/%m/%d")
    archive_dir.mkdir(parents=True, exist_ok=True)
    archive_path = archive_dir / f"petcast_{now.strftime('%H%M%S')}.png"
    final.save(archive_path, "PNG")

    # Symlink latest.png -> archive copy
    latest = config.output.latest
    latest.unlink(missing_ok=True)
    os.symlink(archive_path.resolve(), latest)

    # Save metadata
    metadata = {
        "generated_at": now.isoformat(),
        "pets": [p.name for p in selection.pets],
        "photo": selection.photo,
        "style": selection.style,
        "weather": dict(forecast),
        "scene": {
            "activity": scene.activity,
            "foreground": scene.foreground,
            "background": scene.background,
            "mood": scene.mood,
            "overlay_position": scene.overlay_position,
        },
    }

    # Also save metadata alongside the archive image
    archive_meta = archive_path.with_suffix(".json")
    with open(archive_meta, "w") as f:
        json.dump(metadata, f, indent=2)

    # Symlink latest.json -> archive metadata
    meta_latest = config.output.metadata
    meta_latest.unlink(missing_ok=True)
    os.symlink(archive_meta.resolve(), meta_latest)

    print(f"Done! Saved to {archive_path}")
    return config.output.latest


def _save_debug(img: Image.Image, debug_dir: Path, name: str) -> None:
    """Save a debug image, converting RGBA to RGB if needed."""
    out = img
    if out.mode == "RGBA":
        bg = Image.new("RGB", out.size, (255, 255, 255))
        bg.paste(out, mask=out.split()[3])
        out = bg
    path = debug_dir / f"{name}.png"
    out.save(path, "PNG")
    print(f"  Debug: {path}")
