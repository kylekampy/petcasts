You are the pragmatic programmer personified.

# Petcast Notes

Petcast is a Python 3.13 app that generates daily AI pet-weather art for an
800x480 Spectra 6 e-ink display.

Main flow:
- `src/petcast/config.py` loads `config.yaml`, optional private overlays, and
  `pets/meta/pets.yaml` into dataclasses.
- `src/petcast/select.py` chooses a reference photo, pets, and art style using
  cooldown history in `pets/state/history.json`.
- `src/petcast/weather.py` fetches Open-Meteo forecast data.
- `src/petcast/scene.py` asks the configured chat model for a structured scene.
- `src/petcast/generate.py` builds the image prompt and calls OpenAI or Gemini.
- `src/petcast/pipeline.py` orchestrates the full run, writes outputs, metadata,
  and history.
- `src/petcast/server.py` exposes `/api/generate`, `/api/status`, `/api/archive`,
  and `/output/...` for the frame.

Useful commands:
- `uv sync`
- `uv run pytest`
- `uv run python -m petcast weather`
- `uv run python -m petcast select --count 10`
- `uv run python -m petcast generate --debug`
- `uv run python -m petcast serve`

Generated/private files:
- `.env` stores API keys and is ignored.
- `config.local.yaml` is for private config overlays and is ignored.
- `output/`, `petcast.db*`, and `pets/state/celebrations.json` are runtime
  state/artifacts.

When adding prompt behavior, keep the scene prompt and final image prompt in
sync. The scene prompt decides the concept; the image prompt enforces exact pet
counts, text rules, crop safety, and weather/celebration requirements.
