"""Lightweight HTTP server for frame-driven generation."""

import json
import threading
import time
from datetime import datetime
from http.server import HTTPServer, SimpleHTTPRequestHandler
from pathlib import Path
from zoneinfo import ZoneInfo

from petcast.pipeline import run


class PetcastHandler(SimpleHTTPRequestHandler):
    """Handles API requests and serves static files from output/."""

    root: Path
    _generating: bool = False
    _lock = threading.Lock()

    def do_POST(self):
        if self.path == "/api/generate":
            self._handle_generate()
        else:
            self.send_error(404)

    def do_GET(self):
        if self.path == "/api/status":
            self._handle_status()
        elif self.path == "/api/archive":
            self._handle_archive_list()
        elif self.path.startswith("/output/"):
            self._serve_output_file()
        else:
            self.send_error(404)

    def _handle_generate(self):
        """Kick off generation in background, return 202 immediately.

        Returns 429 if already generated today (unless force=true in body).
        Returns 409 if a generation is already in progress.
        Returns 202 if generation started.
        """
        # Parse body
        battery_pct = None
        force = False
        content_length = int(self.headers.get("Content-Length", 0))
        if content_length > 0:
            try:
                body = json.loads(self.rfile.read(content_length))
                battery_pct = body.get("battery")
                force = body.get("force", False)
            except (json.JSONDecodeError, ValueError):
                pass

        # Check if already generated today (skip if force=true)
        if not force and self._already_generated_today():
            self._json_response(429, {
                "status": "already_generated_today",
                "message": "Already generated an image today. Pass force=true to override.",
            })
            return

        with PetcastHandler._lock:
            if PetcastHandler._generating:
                self._json_response(409, {
                    "status": "already_generating",
                    "message": "A generation is already in progress",
                })
                return
            PetcastHandler._generating = True

        # Return 202 immediately
        self._json_response(202, {
            "status": "accepted",
            "message": "Generation started",
        })

        # Run pipeline in background thread
        thread = threading.Thread(
            target=self._run_pipeline,
            args=(battery_pct,),
            daemon=True,
        )
        thread.start()

    def _already_generated_today(self) -> bool:
        """Check if an image was already generated today in the location's timezone."""
        metadata_path = self.root / "output" / "latest.json"
        if not metadata_path.exists():
            return False
        try:
            with open(metadata_path) as f:
                metadata = json.load(f)
            generated_at = metadata.get("generated_at", "")
            tz_name = metadata.get("weather", {}).get("timezone", "UTC")
            tz = ZoneInfo(tz_name)
            generated_date = datetime.fromisoformat(generated_at).date()
            today = datetime.now(tz).date()
            return generated_date == today
        except (ValueError, KeyError):
            return False

    def _run_pipeline(self, battery_pct: float | None = None):
        """Run the generation pipeline in background."""
        try:
            if battery_pct is not None:
                print(f"[server] Starting generation (battery: {battery_pct:.0f}%)...")
            else:
                print(f"[server] Starting generation...")
            start = time.time()
            run(self.root, debug=False, battery_pct=battery_pct)
            elapsed = time.time() - start
            print(f"[server] Generation complete in {elapsed:.1f}s")
        except Exception as e:
            print(f"[server] Generation failed: {e}")
        finally:
            with PetcastHandler._lock:
                PetcastHandler._generating = False

    def _handle_status(self):
        """Return latest.json metadata if it exists."""
        metadata_path = self.root / "output" / "latest.json"
        if metadata_path.exists():
            with open(metadata_path) as f:
                metadata = json.load(f)
            with PetcastHandler._lock:
                generating = PetcastHandler._generating
            metadata["generating"] = generating
            self._json_response(200, metadata)
        else:
            self._json_response(200, {
                "status": "no_image",
                "generating": PetcastHandler._generating,
            })

    def _handle_archive_list(self):
        """List all archived images with their metadata."""
        archive_dir = self.root / "output" / "archive"
        images = []
        if archive_dir.exists():
            for png in sorted(archive_dir.rglob("*.png"), reverse=True):
                rel = png.relative_to(self.root / "output")
                entry = {"url": f"/output/{rel}", "file": str(rel)}
                # Include metadata if available
                meta_path = png.with_suffix(".json")
                if meta_path.exists():
                    with open(meta_path) as f:
                        entry["metadata"] = json.load(f)
                images.append(entry)
        self._json_response(200, {"images": images})

    def _serve_output_file(self):
        """Serve files from the output directory."""
        # Strip /output/ prefix to get relative path
        rel_path = self.path[len("/output/"):]
        file_path = self.root / "output" / rel_path

        if not file_path.exists() or not file_path.is_file():
            self.send_error(404)
            return

        # Determine content type
        suffix = file_path.suffix.lower()
        content_types = {
            ".png": "image/png",
            ".json": "application/json",
            ".jpg": "image/jpeg",
        }
        content_type = content_types.get(suffix, "application/octet-stream")

        self.send_response(200)
        self.send_header("Content-Type", content_type)
        data = file_path.read_bytes()
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def _json_response(self, code: int, data: dict):
        body = json.dumps(data).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, format, *args):
        print(f"[server] {args[0]}")


def _generate_test_pattern(root: Path) -> None:
    """Generate a Spectra 6 test pattern PNG."""
    from PIL import Image, ImageDraw, ImageFont

    colors = [
        ((255, 0, 0), "Red"),
        ((0, 255, 0), "Green"),
        ((0, 0, 255), "Blue"),
        ((255, 255, 0), "Yellow"),
        ((0, 0, 0), "Black"),
        ((255, 255, 255), "White"),
    ]

    w, h = 800, 480
    img = Image.new("RGB", (w, h), (255, 255, 255))
    draw = ImageDraw.Draw(img)

    bar_w = w // 6
    for i, (color, name) in enumerate(colors):
        x0 = i * bar_w
        x1 = (i + 1) * bar_w if i < 5 else w
        draw.rectangle([x0, 0, x1, h * 2 // 3], fill=color)
        text_color = (255, 255, 255) if name in ("Black", "Red", "Blue", "Green") else (0, 0, 0)
        try:
            font = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", 18)
        except OSError:
            try:
                font = ImageFont.truetype("/System/Library/Fonts/Helvetica.ttc", 18)
            except OSError:
                font = ImageFont.load_default(18)
        bbox = draw.textbbox((0, 0), name, font=font)
        tw = bbox[2] - bbox[0]
        draw.text((x0 + (bar_w - tw) // 2, 10), name, fill=text_color, font=font)

    # Bottom third: checkerboard dither patterns
    patterns = [
        ("Orange", (255, 0, 0), (255, 255, 0)),
        ("Purple", (255, 0, 0), (0, 0, 255)),
        ("Cyan", (0, 255, 0), (0, 0, 255)),
        ("Gray", (0, 0, 0), (255, 255, 255)),
        ("Pink", (255, 0, 0), (255, 255, 255)),
        ("Lime", (0, 255, 0), (255, 255, 255)),
    ]

    pat_w = w // 6
    y_start = h * 2 // 3
    for i, (name, c1, c2) in enumerate(patterns):
        x0 = i * pat_w
        for py in range(y_start, h):
            for px in range(x0, min(x0 + pat_w, w)):
                if (px + py) % 2 == 0:
                    img.putpixel((px, py), c1)
                else:
                    img.putpixel((px, py), c2)
        text_color = (255, 255, 255) if name in ("Purple", "Gray") else (0, 0, 0)
        bbox = draw.textbbox((0, 0), name, font=font)
        tw = bbox[2] - bbox[0]
        draw.text((x0 + (pat_w - tw) // 2, y_start + 10), name, fill=text_color, font=font)

    out = root / "output" / "test_pattern.png"
    out.parent.mkdir(parents=True, exist_ok=True)
    img.save(out, "PNG")
    print(f"[server] Test pattern saved to {out}")


def serve(root: Path, port: int = 7777):
    """Start the petcast HTTP server."""
    PetcastHandler.root = root.resolve()
    _generate_test_pattern(root.resolve())

    server = HTTPServer(("0.0.0.0", port), PetcastHandler)
    print(f"Petcast server listening on port {port}")
    print(f"  POST /api/generate    — trigger image generation")
    print(f"  GET  /api/status      — check status / latest metadata")
    print(f"  GET  /output/latest.png — fetch the latest image")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\nShutting down...")
        server.shutdown()
