"""Open-Meteo weather client."""

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


class Forecast(TypedDict):
    weather_code: int
    weather_desc: str
    weather_icon: str
    high_f: float
    low_f: float
    precip_chance: int
    wind_mph: float
    sunrise: str
    sunset: str


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
        "temperature_unit": "fahrenheit",
        "wind_speed_unit": "mph",
        "timezone": "auto",
        "forecast_days": 1,
    }

    resp = httpx.get("https://api.open-meteo.com/v1/forecast", params=params)
    resp.raise_for_status()
    data = resp.json()

    daily = data["daily"]
    code = daily["weather_code"][0]

    return Forecast(
        weather_code=code,
        weather_desc=WMO_CODES.get(code, f"Unknown ({code})"),
        weather_icon=WMO_ICONS.get(code, "?"),
        high_f=daily["temperature_2m_max"][0],
        low_f=daily["temperature_2m_min"][0],
        precip_chance=daily["precipitation_probability_max"][0],
        wind_mph=daily["wind_speed_10m_max"][0],
        sunrise=daily["sunrise"][0],
        sunset=daily["sunset"][0],
    )
