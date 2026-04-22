"""Open-Meteo weather client."""

from collections import Counter
from datetime import datetime
from typing import TypedDict

import httpx

from petcast.config import Config

# WMO weather codes to descriptions
WMO_CODES: dict[int, str] = {
    0: "Clear sky",
    1: "Mainly clear",
    2: "Partly cloudy",
    3: "Overcast",
    45: "Foggy",
    48: "Depositing rime fog",
    51: "Light drizzle",
    53: "Moderate drizzle",
    55: "Dense drizzle",
    56: "Light freezing drizzle",
    57: "Dense freezing drizzle",
    61: "Slight rain",
    63: "Moderate rain",
    65: "Heavy rain",
    66: "Light freezing rain",
    67: "Heavy freezing rain",
    71: "Slight snowfall",
    73: "Moderate snowfall",
    75: "Heavy snowfall",
    77: "Snow grains",
    80: "Slight rain showers",
    81: "Moderate rain showers",
    82: "Violent rain showers",
    85: "Slight snow showers",
    86: "Heavy snow showers",
    95: "Thunderstorm",
    96: "Thunderstorm with slight hail",
    99: "Thunderstorm with heavy hail",
}

# Simplified weather icons (unicode)
WMO_ICONS: dict[int, str] = {
    0: "\u2600",   # ☀
    1: "\U0001f324",   # 🌤
    2: "\u26c5",   # ⛅
    3: "\u2601",   # ☁
    45: "\U0001f32b",  # 🌫
    48: "\U0001f32b",
    51: "\U0001f326",  # 🌦
    53: "\U0001f326",
    55: "\U0001f327",  # 🌧
    61: "\U0001f327",
    63: "\U0001f327",
    65: "\U0001f327",
    80: "\U0001f326",
    81: "\U0001f327",
    82: "\U0001f327",
    71: "\U0001f328",  # 🌨
    73: "\U0001f328",
    75: "\U0001f328",
    85: "\U0001f328",
    86: "\U0001f328",
    95: "\u26c8",   # ⛈
    96: "\u26c8",
    99: "\u26c8",
}


# Exact icon descriptions for the image generator — no ambiguity
ICON_DESCRIPTIONS: dict[int, str] = {
    0: "a bright yellow sun with rays, no clouds",
    1: "a yellow sun with one small white cloud partially covering it",
    2: "a yellow sun half-hidden behind a white cloud",
    3: "two or three plain gray/white clouds, NO rain, NO sun",
    45: "wavy horizontal fog lines",
    48: "wavy horizontal fog lines",
    51: "a cloud with a few small raindrops",
    53: "a cloud with several raindrops",
    55: "a dark cloud with heavy raindrops",
    56: "a cloud with small icy raindrops",
    57: "a dark cloud with icy raindrops",
    61: "a cloud with a few raindrops",
    63: "a cloud with several raindrops",
    65: "a dark cloud with heavy rain lines",
    66: "a cloud with icy raindrops",
    67: "a dark cloud with heavy icy rain",
    71: "a cloud with a few small snowflakes falling",
    73: "a cloud with several snowflakes falling",
    75: "a dark cloud with heavy snowflakes",
    77: "a cloud with tiny snow grains",
    80: "a cloud with a few raindrops",
    81: "a cloud with several raindrops",
    82: "a dark cloud with heavy rain lines",
    85: "a cloud with a few snowflakes",
    86: "a dark cloud with heavy snowflakes",
    95: "a dark cloud with a yellow lightning bolt",
    96: "a dark cloud with lightning and small hailstones",
    99: "a dark cloud with lightning and large hailstones",
}


class Forecast(TypedDict):
    weather_code: int
    weather_desc: str
    weather_icon: str
    weather_icon_desc: str  # exact description for image generation
    high_f: float
    low_f: float
    precip_chance: int
    wind_mph: float
    sunrise: str
    sunset: str
    timezone: str


def fetch_forecast(config: Config) -> Forecast:
    """Fetch today's forecast from Open-Meteo."""
    params = {
        "latitude": config.location.latitude,
        "longitude": config.location.longitude,
        "daily": ",".join([
            "weather_code",
            "temperature_2m_max",
            "temperature_2m_min",
            "precipitation_probability_max",
            "wind_speed_10m_max",
            "sunrise",
            "sunset",
        ]),
        "hourly": "weather_code",
        "temperature_unit": "fahrenheit",
        "wind_speed_unit": "mph",
        "timezone": "auto",
        "forecast_days": 1,
    }

    resp = httpx.get("https://api.open-meteo.com/v1/forecast", params=params)
    resp.raise_for_status()
    data = resp.json()

    daily = data["daily"]
    sunrise = daily["sunrise"][0]
    sunset = daily["sunset"][0]
    code = _dominant_daylight_code(data.get("hourly", {}), sunrise, sunset, daily["weather_code"][0])

    return Forecast(
        weather_code=code,
        weather_desc=WMO_CODES.get(code, f"Unknown ({code})"),
        weather_icon=WMO_ICONS.get(code, "?"),
        high_f=daily["temperature_2m_max"][0],
        low_f=daily["temperature_2m_min"][0],
        precip_chance=daily["precipitation_probability_max"][0],
        wind_mph=daily["wind_speed_10m_max"][0],
        sunrise=sunrise,
        sunset=sunset,
        timezone=data.get("timezone", "UTC"),
        weather_icon_desc=ICON_DESCRIPTIONS.get(code, "a plain cloud"),
    )


def _dominant_daylight_code(hourly: dict, sunrise: str, sunset: str, fallback: int) -> int:
    """Return the most representative weather code during daylight hours.

    Rules:
      1. Any thunderstorm hour (95-99) wins — always depict storms.
      2. Precipitation codes (>=51) win if ≥2 daylight hours have them (most common wins).
      3. Otherwise return the mode of daylight hours, breaking ties by most severe.
    """
    times = hourly.get("time") or []
    codes = hourly.get("weather_code") or []
    if not times or not codes or len(times) != len(codes):
        return fallback

    try:
        sr = datetime.fromisoformat(sunrise)
        ss = datetime.fromisoformat(sunset)
    except ValueError:
        return fallback

    daylight_codes = [
        c for t, c in zip(times, codes)
        if sr <= datetime.fromisoformat(t) <= ss
    ]
    if not daylight_codes:
        return fallback

    if any(c >= 95 for c in daylight_codes):
        storm = [c for c in daylight_codes if c >= 95]
        return Counter(storm).most_common(1)[0][0]

    precip = [c for c in daylight_codes if c >= 51]
    if len(precip) >= 2:
        return Counter(precip).most_common(1)[0][0]

    counts = Counter(daylight_codes)
    max_count = max(counts.values())
    winners = [c for c, n in counts.items() if n == max_count]
    return max(winners)
