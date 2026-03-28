package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kylekampy/petcasts/internal/config"
	"github.com/kylekampy/petcasts/internal/db"
	"github.com/kylekampy/petcasts/internal/gemini"
	"github.com/kylekampy/petcasts/internal/storage"
)

// GeminiClient is the interface the pipeline needs from the Gemini API.
type GeminiClient interface {
	GenerateText(model string, prompt string) (string, error)
	GenerateImage(model string, prompt string, refImage []byte, refMimeType string, aspectRatio string) (*gemini.GenerateImageResponse, error)
}

// Selection holds the chosen pets, reference photo, and art style.
type Selection struct {
	Pets  []*config.Pet
	Photo string
	Style string
}

// SceneDescription is the structured output from the scene planner.
type SceneDescription struct {
	Activity           string `json:"activity"`
	Foreground         string `json:"foreground"`
	Background         string `json:"background"`
	Mood               string `json:"mood"`
	Constraints        string `json:"constraints"`
	WeatherIntegration string `json:"weather_integration"`
}

// Pipeline orchestrates the full generation flow.
type Pipeline struct {
	Config  *config.Config
	DB      *db.DB
	Store   *storage.Local
	Gemini  GeminiClient
	Logger  *slog.Logger
}

// Result contains the outputs of a pipeline run.
type Result struct {
	OriginalPath string
	DitheredPath string
	Selection    Selection
	Scene        SceneDescription
	Forecast     *Forecast
}

// Run executes the full pipeline: select → weather → scene → image → dither → save.
func (p *Pipeline) Run(frameID string, batteryPct *float64, forceRegen bool) (*Result, error) {
	start := time.Now()

	// Step 1: Select pets and style
	p.Logger.Info("selecting pets and style...")
	sel, err := p.selectPetsAndStyle()
	if err != nil {
		return nil, fmt.Errorf("select: %w", err)
	}
	p.Logger.Info("selected",
		"pets", petNames(sel.Pets),
		"photo", sel.Photo,
		"style_prefix", truncate(sel.Style, 60),
	)

	// Step 2: Fetch weather
	p.Logger.Info("fetching weather...")
	forecast, err := FetchForecast(p.Config)
	if err != nil {
		return nil, fmt.Errorf("weather: %w", err)
	}
	p.Logger.Info("weather",
		"desc", forecast.WeatherDesc,
		"high", fmt.Sprintf("%.0f°F", forecast.HighF),
		"low", fmt.Sprintf("%.0f°F", forecast.LowF),
	)

	// Step 3: Generate scene description
	p.Logger.Info("generating scene description...")
	scene, err := p.generateScene(sel, forecast, batteryPct)
	if err != nil {
		return nil, fmt.Errorf("scene: %w", err)
	}
	p.Logger.Info("scene", "activity", scene.Activity)

	// Step 4: Generate image
	p.Logger.Info("generating image...")
	rawImage, err := p.generateImage(sel, scene, forecast, batteryPct)
	if err != nil {
		return nil, fmt.Errorf("image: %w", err)
	}
	p.Logger.Info("image generated", "size", fmt.Sprintf("%dx%d", rawImage.Bounds().Dx(), rawImage.Bounds().Dy()))

	// Step 5: Dither
	p.Logger.Info("dithering for display...")
	dithered := DitherForDisplay(rawImage, p.Config.Display.Width, p.Config.Display.Height)

	// Step 6: Save
	p.Logger.Info("saving outputs...")
	now := time.Now()
	dateStr := now.Format("2006/01/02")
	timeStr := now.Format("150405")

	// Encode original
	var origBuf bytes.Buffer
	if err := png.Encode(&origBuf, rawImage); err != nil {
		return nil, fmt.Errorf("encode original: %w", err)
	}
	origPath := filepath.Join("generations", dateStr, timeStr+"_original.png")
	if _, err := p.Store.Save(origPath, origBuf.Bytes()); err != nil {
		return nil, fmt.Errorf("save original: %w", err)
	}

	// Encode dithered
	var dithBuf bytes.Buffer
	if err := png.Encode(&dithBuf, dithered); err != nil {
		return nil, fmt.Errorf("encode dithered: %w", err)
	}
	dithPath := filepath.Join("generations", dateStr, timeStr+"_dithered.png")
	if _, err := p.Store.Save(dithPath, dithBuf.Bytes()); err != nil {
		return nil, fmt.Errorf("save dithered: %w", err)
	}

	// Record in DB
	forecastJSON, _ := json.Marshal(forecast)
	sceneJSON, _ := json.Marshal(scene)
	petsJSON, _ := json.Marshal(petNames(sel.Pets))

	if err := p.DB.RecordGeneration(&db.Generation{
		FrameID:      frameID,
		CreatedAt:    now,
		Pets:         string(petsJSON),
		Style:        sel.Style,
		SceneJSON:    string(sceneJSON),
		OriginalPath: origPath,
		DitheredPath: dithPath,
		WeatherJSON:  string(forecastJSON),
		ForceRegen:   forceRegen,
	}); err != nil {
		p.Logger.Error("failed to record generation", "error", err)
	}

	if err := p.DB.RecordSelection(
		strings.Join(petNames(sel.Pets), ","),
		sel.Photo,
		sel.Style,
		scene.Activity,
	); err != nil {
		p.Logger.Error("failed to record selection", "error", err)
	}

	elapsed := time.Since(start)
	p.Logger.Info("pipeline complete", "elapsed", elapsed.Round(time.Millisecond))

	return &Result{
		OriginalPath: origPath,
		DitheredPath: dithPath,
		Selection:    *sel,
		Scene:        *scene,
		Forecast:     forecast,
	}, nil
}

// --- Selection ---

func (p *Pipeline) selectPetsAndStyle() (*Selection, error) {
	photoToPets := p.Config.PhotoToPets()
	allPhotos := make([]string, 0, len(photoToPets))
	for photo := range photoToPets {
		allPhotos = append(allPhotos, photo)
	}
	if len(allPhotos) == 0 {
		return nil, fmt.Errorf("no photos configured")
	}

	// Get recent history for cooldowns
	history, err := p.DB.GetSelectionHistory(90)
	if err != nil {
		p.Logger.Warn("failed to load history, proceeding without cooldowns", "error", err)
	}

	now := time.Now()
	recentPhotos := make(map[string]bool)
	recentStyles := make(map[string]int)
	for _, entry := range history {
		age := now.Sub(entry.CreatedAt)
		if age < time.Duration(p.Config.Cooldowns.PhotoDays)*24*time.Hour {
			recentPhotos[entry.Photo] = true
		}
		if age < time.Duration(p.Config.Cooldowns.ComboDays)*24*time.Hour && entry.Style != "" {
			recentStyles[entry.Style]++
		}
	}

	// Pick photo not recently used
	available := make([]string, 0)
	for _, photo := range allPhotos {
		if !recentPhotos[photo] {
			available = append(available, photo)
		}
	}
	if len(available) == 0 {
		available = allPhotos
	}
	chosenPhoto := available[rand.IntN(len(available))]
	chosenPets := photoToPets[chosenPhoto]

	// Pick style with weighted selection (prefer less-used)
	weights := make([]float64, len(p.Config.Styles))
	for i, style := range p.Config.Styles {
		uses := recentStyles[style]
		w := float64(p.Config.Cooldowns.StyleUses - uses)
		if w < 1 {
			w = 1
		}
		weights[i] = w
	}
	chosenStyle := p.Config.Styles[weightedChoice(weights)]

	return &Selection{Pets: chosenPets, Photo: chosenPhoto, Style: chosenStyle}, nil
}

func weightedChoice(weights []float64) int {
	total := 0.0
	for _, w := range weights {
		total += w
	}
	r := rand.Float64() * total
	cumulative := 0.0
	for i, w := range weights {
		cumulative += w
		if r <= cumulative {
			return i
		}
	}
	return len(weights) - 1
}

// --- Scene Generation ---

const sceneSystemPrompt = `You are a creative director for an illustrated pet weather forecast displayed on a small e-ink frame. You design charming, anthropomorphic scenes where the pets act like little people — doing human activities appropriate to the weather and season.

ANTHROPOMORPHISM: The pets should be doing human-like things. NOT just sitting, lying, or walking. Think: reading a book on the porch, sipping from a mug, grilling in the backyard, building a snowman, flying a kite, having a tea party, fishing, gardening, stargazing, playing board games. They can hold objects, wear accessories, sit in chairs, use tools. Make it whimsical and heartwarming.

IMPORTANT: The art style describes HOW the image is rendered, not WHAT the pets are doing. The pets are the SUBJECTS depicted in that style — they are NOT creating art or doing anything related to the style itself. For example, "graffiti style" means the image looks like spray paint on a wall, NOT that the pets are painting graffiti.

SEASONAL ACCURACY: The scene must match the actual season and weather at the location. In the upper Midwest in early spring, trees are bare, grass is brown/dormant. In summer, everything is lush and green. In fall, leaves are changing. In winter, there's snow. Don't depict lush green when the season doesn't support it.

ART STYLE: You will be given a specific art style. In your descriptions, lean HEAVILY into the visual characteristics of that style. Describe specific techniques, textures, and visual elements unique to that medium.

COMPOSITION: The image will be cropped to a wider aspect ratio — top and bottom ~7% will be cut off. Keep all important elements in the central 70% vertically. The weather info will be creatively integrated into the scene (not necessarily a panel).

CRITICAL RULES:
- Each pet appears EXACTLY ONCE. Never duplicate a pet.
- Each pet has exactly ONE head.
- Name every pet by name in the activity description.
- No border or frame around the image.

Respond with ONLY a JSON object (no markdown fencing) with these keys:
- activity: what the pets are doing (1 sentence, must name ALL pets, must be anthropomorphic)
- foreground: detailed foreground description leaning heavily into the art style's visual language
- background: detailed background (seasonally accurate, described in the art style's visual language)
- mood: lighting and color mood specific to the art style
- constraints: composition notes
- weather_integration: creative idea for how to show the weather info in the scene`

func (p *Pipeline) generateScene(sel *Selection, forecast *Forecast, batteryPct *float64) (*SceneDescription, error) {
	petDescs := make([]string, len(sel.Pets))
	for i, pet := range sel.Pets {
		petDescs[i] = fmt.Sprintf("- %s: %s", pet.Name, pet.Description)
	}

	// Get recent activities to avoid repetition
	history, _ := p.DB.GetSelectionHistory(5)
	recentActivities := make([]string, 0)
	for _, entry := range history {
		if entry.SceneActivity != "" {
			recentActivities = append(recentActivities, entry.SceneActivity)
		}
	}
	recentJSON, _ := json.Marshal(recentActivities)

	now := time.Now()
	season := getSeason(now)
	allNames := petNames(sel.Pets)

	prompt := fmt.Sprintf(`%s

Pets (ALL must appear in the scene):
%s

Date: %s
Season: %s
Location: %s

Weather: %s, high %.0f°F / low %.0f°F, %d%% chance of precipitation, wind %.0f mph.
Sunrise: %s, Sunset: %s

Art style: %s

Recent scenes to AVOID repeating:
%s

Design an anthropomorphic scene featuring ALL pets (%s) for today's forecast image. The pets should be doing something human-like and charming that fits the weather. The environment must reflect the actual season and weather at this location. Lean heavily into the visual language of the art style in your descriptions.`,
		sceneSystemPrompt,
		strings.Join(petDescs, "\n"),
		now.Format("January 2, 2006"),
		season,
		p.Config.Location.Name,
		forecast.WeatherDesc, forecast.HighF, forecast.LowF,
		forecast.PrecipChance, forecast.WindMPH,
		forecast.Sunrise, forecast.Sunset,
		sel.Style,
		string(recentJSON),
		strings.Join(allNames, ", "),
	)

	if batteryPct != nil && *batteryPct < 15 {
		prompt += fmt.Sprintf(`

IMPORTANT: The display frame's battery is critically low at %.0f%%! The pets should look worried, anxious, or concerned about running out of energy. Maybe they're huddled around a dying campfire, or looking at a dimming lantern, or one of them is holding a nearly-empty battery.`, *batteryPct)
	}

	text, err := p.Gemini.GenerateText(p.Config.Gemini.ChatModel, prompt)
	if err != nil {
		return nil, fmt.Errorf("scene generation: %w", err)
	}

	// Strip markdown fencing if present
	raw := strings.TrimSpace(text)
	if strings.HasPrefix(raw, "```") {
		if idx := strings.Index(raw[3:], "\n"); idx >= 0 {
			raw = raw[3+idx+1:]
		}
		if strings.HasSuffix(raw, "```") {
			raw = raw[:len(raw)-3]
		}
		raw = strings.TrimSpace(raw)
	}

	var scene SceneDescription
	if err := json.Unmarshal([]byte(raw), &scene); err != nil {
		return nil, fmt.Errorf("parse scene JSON: %w (raw: %s)", err, truncate(raw, 200))
	}
	return &scene, nil
}

// --- Image Generation ---

func (p *Pipeline) generateImage(sel *Selection, scene *SceneDescription, forecast *Forecast, batteryPct *float64) (image.Image, error) {
	prompt := buildImagePrompt(sel, scene, forecast, batteryPct)

	// Read reference photo
	var refImage []byte
	var refMime string
	photoPath := filepath.Join(p.Config.DataDir, "pets", "input", sel.Photo)
	if data, err := os.ReadFile(photoPath); err == nil {
		refImage = data
		switch strings.ToLower(filepath.Ext(sel.Photo)) {
		case ".jpg", ".jpeg":
			refMime = "image/jpeg"
		default:
			refMime = "image/png"
		}
	}

	resp, err := p.Gemini.GenerateImage(
		p.Config.Gemini.ImageModel,
		prompt,
		refImage,
		refMime,
		"3:2",
	)
	if err != nil {
		return nil, err
	}

	img, _, err := image.Decode(bytes.NewReader(resp.ImageData))
	if err != nil {
		return nil, fmt.Errorf("decode generated image: %w", err)
	}
	return img, nil
}

func buildImagePrompt(sel *Selection, scene *SceneDescription, forecast *Forecast, batteryPct *float64) string {
	numPets := len(sel.Pets)
	countWord := map[int]string{1: "ONE", 2: "TWO", 3: "THREE", 4: "FOUR", 5: "FIVE"}[numPets]
	if countWord == "" {
		countWord = fmt.Sprintf("%d", numPets)
	}

	petList := make([]string, numPets)
	for i, pet := range sel.Pets {
		petList[i] = fmt.Sprintf("  %d. %s: %s", i+1, pet.Name, pet.Description)
	}

	now := time.Now()
	dayName := now.Format("Monday")
	monthName := now.Format("January")
	dayNum := now.Day()
	tempStr := fmt.Sprintf("%.0f°/%.0f°", forecast.HighF, forecast.LowF)

	prompt := fmt.Sprintf(`ART STYLE — THIS IS THE MOST IMPORTANT INSTRUCTION:
%s

The image MUST look like it was physically created using the medium described above. Not a digital illustration with a style filter — it should look like the REAL THING. The medium IS the image. Every pixel should reinforce the chosen art style.

SCENE: %s

EXACTLY %s (%d) PETS — no more, no less. Each appears ONCE:
%s

Use the reference photo to match each pet's appearance. Every pet has exactly ONE head and ONE body. Do NOT duplicate any pet. Count the pets in your output — there must be exactly %d.

The pets are anthropomorphic — they can hold objects, sit in chairs, wear accessories, and do human activities.

FOREGROUND: %s
BACKGROUND: %s
MOOD: %s
COMPOSITION: %s

WEATHER INFO: Creatively incorporate today's forecast into the scene. Must include:
- A weather ICON (visual, not text): %s
- The date: %s, %s %d
- Temperature: %s`,
		sel.Style,
		scene.Activity,
		countWord, numPets,
		strings.Join(petList, "\n"),
		numPets,
		scene.Foreground,
		scene.Background,
		scene.Mood,
		scene.Constraints,
		forecast.WeatherIconDesc,
		dayName, monthName, dayNum,
		tempStr,
	)

	if batteryPct != nil && *batteryPct < 15 {
		prompt += fmt.Sprintf("\n- A low battery warning (%.0f%%)", *batteryPct)
	}

	prompt += `
Integrate these naturally in whatever way fits the art style — a sign, chalkboard, newspaper, banner, window, sky writing, etc.

RULES:
- Exactly ` + fmt.Sprintf("%d", numPets) + ` pets, each with ONE head. No duplicates.
- No text except the weather info.
- Season and weather reflected in the environment.
- Do NOT add any border, frame, or white edge around the image.
- IMPORTANT: This image will be generated at 3:2 aspect ratio but CROPPED to 5:3 (800x480). That means ~10% of the top and bottom will be cut off. Keep ALL important content within the center 80% of the image vertically.`

	return prompt
}

// --- Helpers ---

func petNames(pets []*config.Pet) []string {
	names := make([]string, len(pets))
	for i, p := range pets {
		names[i] = p.Name
	}
	return names
}

func getSeason(t time.Time) string {
	switch t.Month() {
	case 3, 4, 5:
		return "spring"
	case 6, 7, 8:
		return "summer"
	case 9, 10, 11:
		return "fall/autumn"
	default:
		return "winter"
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
