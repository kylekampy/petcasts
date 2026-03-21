"""Lightweight HTTP server for frame-driven generation."""

import json
import threading
import time
from http.server import HTTPServer, SimpleHTTPRequestHandler
from pathlib import Path

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
        """Kick off generation in background, return 202 immediately."""
        with PetcastHandler._lock:
            if PetcastHandler._generating:
                self._json_response(409, {
                    "status": "already_generating",
                    "message": "A generation is already in progress",
                })
                return
            PetcastHandler._generating = True

        # Parse optional battery percentage from POST body
        battery_pct = None
        content_length = int(self.headers.get("Content-Length", 0))
        if content_length > 0:
            try:
                body = json.loads(self.rfile.read(content_length))
                battery_pct = body.get("battery")
            except (json.JSONDecodeError, ValueError):
                pass

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


def serve(root: Path, port: int = 7777):
    """Start the petcast HTTP server."""
    PetcastHandler.root = root.resolve()

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
