"""Orchestrates the full petcast pipeline."""

import json
import os
import time
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


def _step(name: str) -> float:
    """Log a step and return the current time."""
    print(f"[{time.strftime('%H:%M:%S')}] {name}")
    return time.time()


def run(root: Path, debug: bool = False, battery_pct: float | None = None) -> Path:
    """Run the full pipeline and return the path to latest.png."""
    pipeline_start = time.time()
    config = load_config(root)

    config.output.debug_dir.mkdir(parents=True, exist_ok=True)
    config.output.latest.parent.mkdir(parents=True, exist_ok=True)

    # Step 1: Select
    t = _step("Selecting pets and style...")
    selection = select(config, root)
    print(f"  Pets: {', '.join(p.name for p in selection.pets)}")
    print(f"  Photo: {selection.photo}")
    print(f"  Style: {selection.style}")
    print(f"  ({time.time() - t:.1f}s)")

    # Step 2: Weather
    t = _step("Fetching weather...")
    forecast = fetch_forecast(config)
    print(f"  {forecast['weather_desc']}, {forecast['high_f']:.0f}°/{forecast['low_f']:.0f}°F")
    print(f"  ({time.time() - t:.1f}s)")

    # Step 3: Scene description
    t = _step("Generating scene description...")
    history = load_history(root)
    scene = generate_scene(config, selection, forecast, history, battery_pct=battery_pct)
    print(f"  Activity: {scene.activity}")
    print(f"  ({time.time() - t:.1f}s)")

    # Step 4: Image generation
    t = _step("Generating image...")
    raw_image = generate_image(config, selection, scene, forecast, root, battery_pct=battery_pct)
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
    tz = ZoneInfo(forecast["timezone"])
    now = datetime.now(tz)
    archive_dir = config.output.archive_dir / now.strftime("%Y/%m/%d")
    archive_dir.mkdir(parents=True, exist_ok=True)
    archive_path = archive_dir / f"petcast_{now.strftime('%H%M%S')}.png"
    final.save(archive_path, "PNG")

    latest = config.output.latest
    latest.unlink(missing_ok=True)
    os.symlink(archive_path.resolve(), latest)

    metadata = {
        "generated_at": now.isoformat(),
        "pets": [p.name for p in selection.pets],
        "photo": selection.photo,
        "style": selection.style,
        "battery_pct": battery_pct,
        "weather": dict(forecast),
        "scene": {
            "activity": scene.activity,
            "foreground": scene.foreground,
            "background": scene.background,
            "mood": scene.mood,
            "overlay_position": scene.overlay_position,
        },
    }

    archive_meta = archive_path.with_suffix(".json")
    with open(archive_meta, "w") as f:
        json.dump(metadata, f, indent=2)

    meta_latest = config.output.metadata
    meta_latest.unlink(missing_ok=True)
    os.symlink(archive_meta.resolve(), meta_latest)
    print(f"  ({time.time() - t:.1f}s)")

    total = time.time() - pipeline_start
    _step(f"Done! Total: {total:.1f}s — saved to {archive_path}")
    return config.output.latest


def _save_debug(img: Image.Image, debug_dir: Path, name: str) -> None:
    out = img
    if out.mode == "RGBA":
        bg = Image.new("RGB", out.size, (255, 255, 255))
        bg.paste(out, mask=out.split()[3])
        out = bg
    path = debug_dir / f"{name}.png"
    out.save(path, "PNG")
    print(f"  Debug: {path}")
