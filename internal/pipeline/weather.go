package pipeline

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/kylekampy/petcasts/internal/config"
)

var weatherHTTPClient = &http.Client{Timeout: 30 * time.Second}

// Forecast holds today's weather data from Open-Meteo.
type Forecast struct {
	WeatherCode     int     `json:"weather_code"`
	WeatherDesc     string  `json:"weather_desc"`
	WeatherIcon     string  `json:"weather_icon"`
	WeatherIconDesc string  `json:"weather_icon_desc"`
	HighF           float64 `json:"high_f"`
	LowF            float64 `json:"low_f"`
	PrecipChance    int     `json:"precip_chance"`
	WindMPH         float64 `json:"wind_mph"`
	Sunrise         string  `json:"sunrise"`
	Sunset          string  `json:"sunset"`
	Timezone        string  `json:"timezone"`
}

var wmoCodes = map[int]string{
	0: "Clear sky", 1: "Mainly clear", 2: "Partly cloudy", 3: "Overcast",
	45: "Foggy", 48: "Depositing rime fog",
	51: "Light drizzle", 53: "Moderate drizzle", 55: "Dense drizzle",
	56: "Light freezing drizzle", 57: "Dense freezing drizzle",
	61: "Slight rain", 63: "Moderate rain", 65: "Heavy rain",
	66: "Light freezing rain", 67: "Heavy freezing rain",
	71: "Slight snowfall", 73: "Moderate snowfall", 75: "Heavy snowfall",
	77: "Snow grains",
	80: "Slight rain showers", 81: "Moderate rain showers", 82: "Violent rain showers",
	85: "Slight snow showers", 86: "Heavy snow showers",
	95: "Thunderstorm", 96: "Thunderstorm with slight hail", 99: "Thunderstorm with heavy hail",
}

var wmoIcons = map[int]string{
	0: "\u2600", 1: "\U0001f324", 2: "\u26c5", 3: "\u2601",
	45: "\U0001f32b", 48: "\U0001f32b",
	51: "\U0001f326", 53: "\U0001f326", 55: "\U0001f327",
	61: "\U0001f327", 63: "\U0001f327", 65: "\U0001f327",
	80: "\U0001f326", 81: "\U0001f327", 82: "\U0001f327",
	71: "\U0001f328", 73: "\U0001f328", 75: "\U0001f328",
	85: "\U0001f328", 86: "\U0001f328",
	95: "\u26c8", 96: "\u26c8", 99: "\u26c8",
}

var iconDescriptions = map[int]string{
	0: "a bright yellow sun with rays, no clouds",
	1: "a yellow sun with one small white cloud partially covering it",
	2: "a yellow sun half-hidden behind a white cloud",
	3: "two or three plain gray/white clouds, NO rain, NO sun",
	45: "wavy horizontal fog lines", 48: "wavy horizontal fog lines",
	51: "a cloud with a few small raindrops", 53: "a cloud with several raindrops",
	55: "a dark cloud with heavy raindrops",
	56: "a cloud with small icy raindrops", 57: "a dark cloud with icy raindrops",
	61: "a cloud with a few raindrops", 63: "a cloud with several raindrops",
	65: "a dark cloud with heavy rain lines",
	66: "a cloud with icy raindrops", 67: "a dark cloud with heavy icy rain",
	71: "a cloud with a few small snowflakes falling",
	73: "a cloud with several snowflakes falling",
	75: "a dark cloud with heavy snowflakes", 77: "a cloud with tiny snow grains",
	80: "a cloud with a few raindrops", 81: "a cloud with several raindrops",
	82: "a dark cloud with heavy rain lines",
	85: "a cloud with a few snowflakes", 86: "a dark cloud with heavy snowflakes",
	95: "a dark cloud with a yellow lightning bolt",
	96: "a dark cloud with lightning and small hailstones",
	99: "a dark cloud with lightning and large hailstones",
}

func FetchForecast(cfg *config.Config) (*Forecast, error) {
	params := url.Values{
		"latitude":       {strconv.FormatFloat(cfg.Location.Latitude, 'f', 4, 64)},
		"longitude":      {strconv.FormatFloat(cfg.Location.Longitude, 'f', 4, 64)},
		"daily":          {"weather_code,temperature_2m_max,temperature_2m_min,precipitation_probability_max,wind_speed_10m_max,sunrise,sunset"},
		"temperature_unit": {"fahrenheit"},
		"wind_speed_unit":  {"mph"},
		"timezone":         {"auto"},
		"forecast_days":    {"1"},
	}

	resp, err := weatherHTTPClient.Get("https://api.open-meteo.com/v1/forecast?" + params.Encode())
	if err != nil {
		return nil, fmt.Errorf("fetch forecast: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read forecast: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("forecast API error (%d): %s", resp.StatusCode, string(body))
	}

	var data struct {
		Timezone string `json:"timezone"`
		Daily    struct {
			WeatherCode   []int     `json:"weather_code"`
			TempMax       []float64 `json:"temperature_2m_max"`
			TempMin       []float64 `json:"temperature_2m_min"`
			PrecipChance  []int     `json:"precipitation_probability_max"`
			WindSpeed     []float64 `json:"wind_speed_10m_max"`
			Sunrise       []string  `json:"sunrise"`
			Sunset        []string  `json:"sunset"`
		} `json:"daily"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("decode forecast: %w", err)
	}

	if len(data.Daily.WeatherCode) == 0 {
		return nil, fmt.Errorf("forecast API returned no daily data")
	}

	code := data.Daily.WeatherCode[0]
	desc := wmoCodes[code]
	if desc == "" {
		desc = fmt.Sprintf("Unknown (%d)", code)
	}
	icon := wmoIcons[code]
	if icon == "" {
		icon = "?"
	}
	iconDesc := iconDescriptions[code]
	if iconDesc == "" {
		iconDesc = "a plain cloud"
	}

	return &Forecast{
		WeatherCode:     code,
		WeatherDesc:     desc,
		WeatherIcon:     icon,
		WeatherIconDesc: iconDesc,
		HighF:           data.Daily.TempMax[0],
		LowF:            data.Daily.TempMin[0],
		PrecipChance:    data.Daily.PrecipChance[0],
		WindMPH:         data.Daily.WindSpeed[0],
		Sunrise:         data.Daily.Sunrise[0],
		Sunset:          data.Daily.Sunset[0],
		Timezone:        data.Timezone,
	}, nil
}
