package gemini

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const baseURL = "https://generativelanguage.googleapis.com/v1beta"

// Client wraps the Gemini REST API.
type Client struct {
	APIKey     string
	HTTPClient *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		APIKey:     apiKey,
		HTTPClient: http.DefaultClient,
	}
}

// Part is a content part (text or inline data).
type Part struct {
	Text       string      `json:"text,omitempty"`
	InlineData *InlineData `json:"inlineData,omitempty"`
}

type InlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"` // base64
}

type Content struct {
	Parts []Part `json:"parts"`
}

type GenerationConfig struct {
	ResponseModalities []string    `json:"responseModalities,omitempty"`
	ImageGenConfig     *ImageGenConfig `json:"imageGenerationConfig,omitempty"`
}

type ImageGenConfig struct {
	AspectRatio string `json:"aspectRatio,omitempty"`
}

type generateRequest struct {
	Contents         []Content         `json:"contents"`
	GenerationConfig *GenerationConfig `json:"generationConfig,omitempty"`
}

type generateResponse struct {
	Candidates []struct {
		Content struct {
			Parts []responsePart `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	Error *apiError `json:"error,omitempty"`
}

type responsePart struct {
	Text       string      `json:"text,omitempty"`
	InlineData *InlineData `json:"inlineData,omitempty"`
}

type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// GenerateText sends a text-only request and returns the text response.
func (c *Client) GenerateText(model string, prompt string) (string, error) {
	req := generateRequest{
		Contents: []Content{{Parts: []Part{{Text: prompt}}}},
	}
	resp, err := c.doGenerate(model, &req)
	if err != nil {
		return "", err
	}
	for _, cand := range resp.Candidates {
		for _, part := range cand.Content.Parts {
			if part.Text != "" {
				return part.Text, nil
			}
		}
	}
	return "", fmt.Errorf("no text in response")
}

// GenerateImageResponse holds both text and image data from an image generation call.
type GenerateImageResponse struct {
	Text      string
	ImageData []byte // raw PNG/JPEG bytes
	MimeType  string
}

// GenerateImage sends a multimodal request (text + optional reference image) and returns the generated image.
func (c *Client) GenerateImage(model string, prompt string, refImage []byte, refMimeType string, aspectRatio string) (*GenerateImageResponse, error) {
	parts := []Part{{Text: prompt}}
	if len(refImage) > 0 {
		parts = append(parts, Part{
			InlineData: &InlineData{
				MimeType: refMimeType,
				Data:     base64.StdEncoding.EncodeToString(refImage),
			},
		})
	}

	genConfig := &GenerationConfig{
		ResponseModalities: []string{"TEXT", "IMAGE"},
	}
	if aspectRatio != "" {
		genConfig.ImageGenConfig = &ImageGenConfig{AspectRatio: aspectRatio}
	}

	req := generateRequest{
		Contents:         []Content{{Parts: parts}},
		GenerationConfig: genConfig,
	}

	resp, err := c.doGenerate(model, &req)
	if err != nil {
		return nil, err
	}

	result := &GenerateImageResponse{}
	for _, cand := range resp.Candidates {
		for _, part := range cand.Content.Parts {
			if part.Text != "" {
				result.Text = part.Text
			}
			if part.InlineData != nil {
				imgBytes, err := base64.StdEncoding.DecodeString(part.InlineData.Data)
				if err != nil {
					return nil, fmt.Errorf("decode image: %w", err)
				}
				result.ImageData = imgBytes
				result.MimeType = part.InlineData.MimeType
			}
		}
	}

	if len(result.ImageData) == 0 {
		return nil, fmt.Errorf("no image in response")
	}
	return result, nil
}

func (c *Client) doGenerate(model string, req *generateRequest) (*generateResponse, error) {
	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", baseURL, model, c.APIKey)

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gemini request: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gemini API error (status %d): %s", httpResp.StatusCode, string(respBody))
	}

	var resp generateResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("gemini error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	return &resp, nil
}
