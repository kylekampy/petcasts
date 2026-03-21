# Petcast

AI-generated daily pet weather forecasts for e-ink displays. Heavily inspired by [forecats](https://github.com/jwardbond/forecats).

Petcast picks your pets, checks the weather, generates a styled scene with an integrated forecast panel via OpenAI, dithers it for a 6-color e-ink palette, and serves it over HTTP for your display to fetch.

![Mumford and Fennec snuggling](docs/samples/mumford_fennec_blanket.png)
![Pixel art cats](docs/samples/pixel_art_cats.png)
![Claymation dogs](docs/samples/claymation_dogs.png)

## How it works

1. **Pick a photo** — randomly selects a reference photo from your collection. The pets in that photo become the cast for the day.
2. **Fetch weather** — pulls today's forecast from [Open-Meteo](https://open-meteo.com/) (free, no API key needed).
3. **Generate a scene** — GPT-4.1 designs a scene based on the pets, weather, season, location, and a randomly chosen art style.
4. **Generate the image** — gpt-image-1.5 renders the scene with a baked-in forecast panel, using the reference photo for pet likeness.
5. **Dither** — Floyd-Steinberg dithers the image to the Spectra 6 e-ink palette (black, white, red, green, blue, yellow) at 800x480.
6. **Serve** — an HTTP server lets your display fetch the image whenever it's ready.

## Quick start

```bash
# Clone and customize
git clone https://github.com/kylekampy/petcasts.git
cd petcasts

# Add your OpenAI API key
echo "OPENAI_API_KEY=sk-..." > .env

# Install deps
uv sync

# Test weather
uv run python -m petcast weather

# Test selection
uv run python -m petcast select --count 10

# Generate an image (with debug output)
uv run python -m petcast generate --debug

# Start the HTTP server
uv run python -m petcast serve
```

## Docker

```bash
# Run from GitHub Container Registry
docker run -d \
  --name petcast \
  -p 7777:7777 \
  -e OPENAI_API_KEY=sk-... \
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

Designed for the [Seeed reTerminal E1002](https://www.seeedstudio.com/reTerminal-E10-2-p-6366.html) (800x480, Spectra 6 color e-ink) running [ESPHome](https://esphome.io/).

### Daily schedule

| Time | Action |
|------|--------|
| 4:58 AM | Wake from deep sleep, POST `/api/generate` to trigger image generation |
| 5:00 AM | GET `/output/latest.png`, update the e-ink display |
| 5:02 AM | Deep sleep until tomorrow |

### Green button

- **Tap** (while asleep): wake up, trigger regeneration, wait 2 minutes, fetch and display the new image.

### Right white button

- **Tap** (while awake): enter deep sleep until 4:58 AM.

### Battery

The frame sends its battery percentage with each generation request. It shows up as a small battery icon in the forecast panel. When battery drops below 15%, the pets in the scene will look worried about running out of energy.

You can test battery behavior locally:

```bash
# Normal battery
uv run python -m petcast generate --debug --battery 72

# Low battery — pets get anxious
uv run python -m petcast generate --debug --battery 8
```

### ESPHome setup

1. Copy `esphome/petcast-frame.yaml` to your ESPHome config directory
2. Create `esphome/secrets.yaml` with your WiFi credentials:
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
4. **Edit `config.yaml`** — set your location (lat/lon), tweak styles, adjust cooldowns
5. **Add your `OPENAI_API_KEY`** as a repo secret (for the GitHub Action) and in `.env` (for local dev)
6. **Push** — the GitHub Action builds and publishes your container to your own ghcr.io registry

### pets.yaml format

```yaml
pets:
  - name: 'Luna'
    description: >-
      A golden retriever. Fluffy cream coat, dark eyes, always smiling.
      Loves swimming and carrying sticks.
    photos:
      - 'luna_and_max.png'
      - 'luna_solo.png'
  - name: 'Max'
    description: >-
      A black lab. Sleek short coat, brown eyes, floppy ears.
      Ball obsessed. Will not give it back.
    photos:
      - 'luna_and_max.png'
      - 'max_sleeping.png'
```

Photos define natural groupings — if Luna and Max appear in `luna_and_max.png`, they'll sometimes be generated together. Solo photos mean solo scenes.

### config.yaml

```yaml
location:
  name: 'Your City'
  latitude: 40.7128
  longitude: -74.0060

styles:
  - 'comic book pop art with bold outlines and flat colors'
  - 'Japanese woodblock print with strong black outlines'
  # ... add styles that work well with e-ink dithering
```

## API

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/generate` | POST | Trigger image generation (returns 202, runs async). Optional JSON body: `{"battery": 85}` |
| `/api/status` | GET | Latest metadata + `generating` flag |
| `/api/archive` | GET | List all archived images with metadata |
| `/output/latest.png` | GET | The latest generated image |
| `/output/latest.json` | GET | The latest metadata |
| `/output/archive/...` | GET | Archived images by date |

## Cost

~$0.06 per generation with gpt-image-1.5. At one image per day, that's about **$1.70/month**.
