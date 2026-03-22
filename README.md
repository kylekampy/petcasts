# Petcast

AI-generated daily pet weather forecasts for e-ink displays. Heavily inspired by [forecats](https://github.com/jwardbond/forecats).

Petcast picks your pets, checks the weather, generates a styled scene via Google Gemini, dithers it for a 6-color e-ink palette, and serves it over HTTP for your display to fetch.

![Graffiti street art — Zeya and Mumford](docs/samples/graffiti_street_art.png)
![Children's book — reading together](docs/samples/childrens_book_reading.png)
![Folk art — low battery](docs/samples/folk_art_low_battery.png)
![Pixel art — porch snack](docs/samples/pixel_art_porch.png)

## How it works

1. **Pick a photo** — randomly selects a reference photo from your collection. The pets in that photo become the cast for the day.
2. **Fetch weather** — pulls today's forecast from [Open-Meteo](https://open-meteo.com/) (free, no API key needed).
3. **Generate a scene** — Gemini 2.5 Flash designs an anthropomorphic scene where the pets do human-like activities (sipping coffee, flying kites, reading books) appropriate to the weather and season, described in the visual language of a randomly chosen art style.
4. **Generate the image** — Gemini 3 Pro renders the scene with creatively integrated weather info (date, temperature, weather icon), using reference photos for pet likeness. The weather info is woven into the art style — on a sign, chalkboard, newspaper, banner, etc.
5. **Dither** — Atkinson dithers the image to the Spectra 6 e-ink palette (black, white, red, green, blue, yellow) at 800x480.
6. **Serve** — an HTTP server lets your display fetch the image whenever it's ready.

## Quick start

```bash
# Clone and customize
git clone https://github.com/kylekampy/petcasts.git
cd petcasts

# Add your Google API key (get one at https://ai.google.dev)
echo "GOOGLE_API_KEY=..." > .env

# Install deps
uv sync

# Test weather
uv run python -m petcast weather

# Test selection
uv run python -m petcast select --count 10

# Generate an image
uv run python -m petcast generate --debug

# Force a specific style
uv run python -m petcast generate --debug --style graffiti

# Test low battery behavior
uv run python -m petcast generate --debug --battery 8

# Start the HTTP server
uv run python -m petcast serve
```

## Docker

```bash
# Run from GitHub Container Registry
docker run -d \
  --name petcast \
  -p 7777:7777 \
  -e GOOGLE_API_KEY=... \
  -v /opt/volumes/petcast/state:/app/pets/state \
  -v /opt/volumes/petcast/output:/app/output \
  --restart unless-stopped \
  ghcr.io/kylekampy/petcasts:latest

# Trigger a generation
curl -X POST http://localhost:7777/api/generate

# Check status
curl http://localhost:7777/api/status

# Fetch the image
curl http://localhost:7777/output/latest.png -o forecast.png

# Browse past images
curl http://localhost:7777/api/archive
```

## Display

Designed for the [Seeed reTerminal E1002](https://www.seeedstudio.com/reTerminal-E1002-p-6533.html) (800x480, Spectra 6 color e-ink) running [ESPHome](https://esphome.io/).

### How it works

The frame wakes from deep sleep once a day, triggers image generation on the server, fetches the result, updates the e-ink display, and goes back to sleep. The whole cycle takes about 2 minutes.

### Buttons

- **Green** (wake): wakes from deep sleep, fetches latest image
- **Right white** (regenerate): triggers a new generation while awake
- **Left white** (test pattern): toggles a color calibration test pattern

### Battery

The frame sends its battery percentage with each generation request. When battery drops below 15%, the pets in the scene look worried about running out of energy — huddling around dying lanterns, clutching dimming flashlights, etc.

### ESPHome setup

1. Copy `esphome/petcast-frame.yaml` to your ESPHome config directory
2. Create `esphome/secrets.yaml`:
   ```yaml
   wifi_ssid: "your-wifi-ssid"
   wifi_password: "your-wifi-password"
   ap_password: "your-fallback-ap-password"
   ```
3. Update the server URL in `petcast-frame.yaml` (default: `http://mini:7777`)
4. Update the timezone (default: `America/Chicago`)
5. Flash to your ESP32-S3

## Fork and make it your own

1. **Fork this repo**
2. **Replace the pet photos** in `pets/input/` with your own
3. **Edit `pets/meta/pets.yaml`** — name each pet, describe their appearance and personality, and list which photos they appear in
4. **Edit `config.yaml`** — set your location, tweak styles, adjust cooldowns
5. **Add your `GOOGLE_API_KEY`** as a repo secret (for the GitHub Action) and in `.env` (for local dev)
6. **Push** — the GitHub Action builds and publishes your container to your own ghcr.io registry

### pets.yaml format

```yaml
pets:
  - name: "Luna"
    description: >-
      A golden retriever. Fluffy cream coat, dark eyes, always smiling.
      Loves swimming and carrying sticks.
    photos:
      - "luna_and_max.png"
      - "luna_solo.png"
  - name: "Max"
    description: >-
      A black lab. Sleek short coat, brown eyes, floppy ears.
      Ball obsessed. Will not give it back.
    photos:
      - "luna_and_max.png"
      - "max_sleeping.png"
```

Photos define natural groupings — if Luna and Max appear in `luna_and_max.png`, they'll sometimes be generated together. Solo photos mean solo scenes.

## API

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/generate` | POST | Trigger image generation (returns 202, runs async). Optional body: `{"battery": 85}` |
| `/api/status` | GET | Latest metadata + `generating` flag |
| `/api/archive` | GET | List all archived images with metadata |
| `/output/latest.png` | GET | The latest generated image |
| `/output/latest.json` | GET | The latest metadata |
| `/output/archive/...` | GET | Archived images by date |

## Cost

~$0.14 per generation (Gemini 3 Pro for the image + Gemini 2.5 Flash for the scene). At one image per day, that's about **$4/month**.
