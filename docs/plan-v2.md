# Petcast v2 — High-Level Plan

## What exists today

A single-user Python prototype running on a Mac Mini. It generates a daily Gemini-illustrated weather scene starring your pets, dithers it for a 6-color Spectra e-ink display (Seeed reTerminal E1002), and serves it over LAN. The ESP32 frame wakes at 5 AM, hits the server, displays the image, and sleeps. Works great for one household. No accounts, no web UI, no billing.

---

## Architecture Overview

```
┌─────────────┐      ┌──────────────────────────────────┐      ┌───────────┐
│  E-ink Frame │◄────►│         Petcast Server (Go)       │◄────►│  GCS      │
│  (ESP32-S3)  │ HTTPS│                                    │      │  Buckets  │
└─────────────┘      │  ┌─────────┐ ┌──────────┐ ┌──────┐│      └───────────┘
                      │  │ API     │ │ Pipeline │ │ Web  ││
                      │  │ (frame) │ │ (gen)    │ │ (app)││      ┌───────────┐
                      │  └─────────┘ └──────────┘ └──────┘│◄────►│  Stripe   │
                      └──────────────────────────────────┘      └───────────┘
                                        │
                                        ▼
                                   ┌──────────┐
                                   │ Gemini   │
                                   │ API      │
                                   └──────────┘
```

**Single Go binary.** Serves the web app, the frame API, and runs the generation pipeline. No separate worker process — generation runs in a goroutine. Cloud SQL (Postgres) for metadata. GCS for images (pet portraits, generated art, dithered output). Deployed to Cloud Run.

---

## Frame Provisioning

### How does a user connect their frame to the service?

1. **User signs up** at petcast.app, sets up pets/styles, subscribes via Stripe
2. Dashboard shows a **pairing code** (e.g., `WREN-4827`) and a **server URL**
3. User **flashes their frame** with Petcast firmware (ESPHome-based, web flasher at petcast.app/flash)
4. Frame boots into **captive portal AP** — user enters WiFi creds + pairing code
5. Frame calls `POST /api/v1/pair` with the code — server returns an API token + frame ID
6. Frame stores token in flash, shows a confirmation image on the display
7. From then on: frame wakes daily → `GET /api/v1/frame/{id}/image` with bearer token → server returns image + JSON headers (`X-OTA-Available`, `X-OTA-URL`) → frame displays image, then checks OTA flag and stays awake for update if needed

For **self-hosters**: same flow, but they point the captive portal at their own server URL instead of petcast.app. The pairing code is just a convenience — self-hosters could also hardcode a token.

### Supported hardware (priority order)

| Frame                               | Price | Display                     | Notes                                    |
| ----------------------------------- | ----- | --------------------------- | ---------------------------------------- |
| **Waveshare ESP32-S3-PhotoPainter** | ~$42  | 7.3" Spectra 6 (800x480)    | Best value, primary target, wood frame   |
| **Seeed reTerminal E1002**          | ~$99  | 7.3" Spectra 6 (800x480)    | Current prototype hardware, better build |
| **Inkplate 13SPECTRA**              | ~$350 | 13.3" Spectra 6 (1600x1200) | Premium, ships mid-2026                  |
| **TRMNL DIY Kit**                   | $45   | 7.5" B&W (800x480)          | Budget option via BYOS protocol          |

Ship ESPHome configs for each. A web flasher (using ESP Web Tools / Improv) lets users flash from the browser with zero toolchain setup.

---

## Data Model

```
users
  id, email, stripe_customer_id, stripe_sub_id, plan, timezone, location

pets
  id, user_id, name, description, portrait_gcs_path

groupings
  id, user_id, name, pet_ids[], weight (frequency)

frames
  id, user_id, api_token_hash, hardware_type, display_w, display_h,
  last_seen_at, battery_pct, firmware_version, stripe_sub_item_id,
  pending_ota_version

styles (system-level, not per-user)
  id, name, prompt_fragment, preview_gcs_path

user_styles
  user_id, style_id, enabled (toggle on/off per user)

generations
  id, user_id, frame_id, created_at, pets[], style_id,
  scene_json, original_gcs_path, dithered_gcs_path,
  weather_json, is_force_regen

daily_state
  frame_id, date, generated (bool), force_regen_used (bool)
```

### Storage layout (GCS)

```
portraits/{user_id}/{pet_id}/{filename}                    — uploaded pet photos
generations/{user_id}/{date}/{timestamp}_original.png      — full-res AI output
generations/{user_id}/{date}/{timestamp}_dithered.png      — frame-ready output
styles/previews/{style_id}.png                             — curated example images for style picker
```

---

## Web App

Server-rendered HTML (Go templates + htmx). No SPA framework.

| Route               | Purpose                                                               |
| ------------------- | --------------------------------------------------------------------- |
| `/`                 | Landing page, pricing, "how it works"                                 |
| `/signup`, `/login` | Auth (Google/Apple OAuth)                                             |
| `/dashboard`        | Today's image, frame status (battery, last seen), force regen button  |
| `/pets`             | Add/edit/remove pets. Upload portrait. Name + personality description |
| `/groups`           | Define which pets appear together, set relative frequency weights     |
| `/styles`           | Grid of style previews (actual sample images). Toggle each on/off     |
| `/history`          | Gallery of past original (not dithered) images                        |
| `/frames`           | Pair new frame, see connected frames, get pairing code                |
| `/settings`         | Location, timezone, billing (Stripe customer portal link)             |

---

## Generation Pipeline (ported to Go)

Same logical steps as the Python prototype, rewritten in Go:

1. **Select** — Pick pet group (weighted random from user's groupings) + style (weighted, cooldown-tracked)
2. **Weather** — Open-Meteo API (free, no key)
3. **Scene** — Gemini Flash chat → structured JSON activity description
4. **Image** — Gemini Pro image generation with pet portrait references
5. **Dither** — Atkinson dithering to target palette (Spectra 6 or B&W depending on frame)
6. **Store** — Upload original + dithered to GCS, write generation record to DB

### Triggering

On-demand only: **frame wakes → hits API → if no image today, generate now → return image**. No pre-generation — users aren't watching the frame update, so the ~2 min generation time doesn't matter. Simpler and no wasted generations for unplugged frames.

### Force regen

One per day per frame. `daily_state` tracks usage per frame ID. Returns 429 if already used.

---

## Billing (Stripe)

- **One plan**: $9/month per frame
- Stripe Checkout for signup, Customer Portal for management
- Webhook listener for `invoice.paid`, `customer.subscription.deleted`, etc.
- Grace period: 3 days past-due before frames stop getting new images (show last generated image with a subtle "subscription expired" overlay)
- **Self-hosters**: no Stripe, no billing code — feature-flagged out. They bring their own Gemini API key

---

## Self-Hosting Story

The Go binary is the same for SaaS and self-hosted. Config-driven:

```yaml
# self-hosted mode
mode: self-hosted
gemini_api_key: ${GEMINI_API_KEY}
storage: local # ./data/ instead of GCS
database: sqlite # instead of Cloud SQL
```

Self-hosters get everything except Stripe billing. Ship a Docker image + docker-compose.yml. Single container, SQLite, local filesystem. Same web UI for managing pets/styles/history.

---

## Repo Structure

```
petcast/
├── cmd/petcast/main.go          # entrypoint
├── internal/
│   ├── server/                  # HTTP server, routes, middleware
│   ├── api/                     # Frame API handlers
│   ├── web/                     # Web app handlers + templates
│   ├── pipeline/                # Generation pipeline
│   │   ├── select.go
│   │   ├── weather.go
│   │   ├── scene.go
│   │   ├── generate.go
│   │   └── dither.go
│   ├── auth/                    # Auth (magic link / OAuth)
│   ├── billing/                 # Stripe integration
│   ├── storage/                 # GCS + local filesystem adapter
│   ├── db/                      # Postgres + SQLite adapter
│   └── frame/                   # Frame pairing, token management
├── web/
│   ├── templates/               # Go HTML templates
│   └── static/                  # CSS, JS, images
├── firmware/
│   ├── waveshare-photopainter/  # ESPHome config
│   ├── seeed-e1002/             # ESPHome config (current)
│   └── shared/                  # Common ESPHome includes
├── deploy/
│   ├── Dockerfile
│   ├── docker-compose.yml       # Self-hosted
│   └── cloudrun/                # GCP deployment config
└── docs/
```

---

## Migration Path from Prototype

The Python prototype stays as-is for personal use while v2 is built. The Go rewrite is a fresh codebase that ports the logic (prompts, dithering algorithm, selection weights) but is architecturally different (multi-tenant, persistent DB, cloud storage). No incremental migration — it's a rewrite with the prototype as reference.

---

## Phased Build Order

### Phase 1 — Core loop

Get one frame working end-to-end on the new stack.

- Go server with frame API (`/pair`, `/image`)
- Generation pipeline (port from Python)
- SQLite + local storage
- Single-user, no auth, no billing
- One ESPHome firmware (Waveshare PhotoPainter)

### Phase 2 — Web dashboard

- Auth (Google/Apple OAuth)
- Pet management (upload portrait, describe personality)
- Group management
- Style picker (with preview images)
- History gallery
- Frame pairing flow

### Phase 3 — Multi-tenant SaaS

- Postgres + GCS
- Stripe billing
- Multi-user isolation
- Cloud Run deployment
- Web flasher for firmware

### Phase 4 — Polish & launch

- Landing page
- Docs site
- Additional frame support (Seeed, Inkplate, TRMNL)
- Self-hosted Docker image + docs

---

## Resolved Decisions

1. **Auth**: Google/Apple OAuth. No magic links or passwords.
2. **Pricing**: $9/month per frame. Generation cost is ~$4.20/mo per frame at current Gemini pricing — leaves room for compute/storage/bandwidth. Gemini credits may further improve margins at scale.
3. **Generation trigger**: On-demand only. Frame wakes → requests image → server generates if needed. No pre-generation. Users aren't watching the frame update, so the ~2 min wait is fine.
4. **OTA firmware updates**: API response includes an `X-OTA-Available` flag. When set, the frame stays awake after displaying the image and enters an OTA update loop to pull new firmware.
5. **Multi-frame**: Per-frame subscription. Each frame has its own ID, token, and billing line item. A user can have multiple frames, each billed separately. All API requests include the frame identifier.
