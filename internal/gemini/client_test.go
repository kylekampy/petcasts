package gemini

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGenerateText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %s, want POST", r.Method)
		}

		// Verify request body
		var req generateRequest
		json.NewDecoder(r.Body).Decode(&req)
		if len(req.Contents) == 0 || len(req.Contents[0].Parts) == 0 {
			t.Error("request has no content parts")
		}
		if req.Contents[0].Parts[0].Text != "hello" {
			t.Errorf("prompt = %q, want %q", req.Contents[0].Parts[0].Text, "hello")
		}

		resp := generateResponse{
			Candidates: []struct {
				Content struct {
					Parts []responsePart `json:"parts"`
				} `json:"content"`
			}{
				{Content: struct {
					Parts []responsePart `json:"parts"`
				}{Parts: []responsePart{{Text: "world"}}}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := &Client{
		APIKey:     "test-key",
		HTTPClient: server.Client(),
	}
	// Override base URL by using the test server
	// We need to patch the URL - use a custom transport
	origURL := baseURL
	defer func() { /* can't restore package-level const, but test server handles it */ }()
	_ = origURL

	// Use a roundtripper that redirects to our test server
	client.HTTPClient = &http.Client{
		Transport: &redirectTransport{
			target: server.URL,
			inner:  http.DefaultTransport,
		},
	}

	text, err := client.GenerateText("test-model", "hello")
	if err != nil {
		t.Fatalf("GenerateText() error: %v", err)
	}
	if text != "world" {
		t.Errorf("GenerateText() = %q, want %q", text, "world")
	}
}

func TestGenerateImage(t *testing.T) {
	fakeImage := []byte{0x89, 0x50, 0x4E, 0x47} // PNG magic bytes (abbreviated)
	fakeImageB64 := base64.StdEncoding.EncodeToString(fakeImage)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req generateRequest
		json.NewDecoder(r.Body).Decode(&req)

		// Verify generation config
		if req.GenerationConfig == nil {
			t.Error("GenerationConfig is nil")
			return
		}
		if len(req.GenerationConfig.ResponseModalities) != 2 {
			t.Errorf("ResponseModalities = %v, want [TEXT, IMAGE]", req.GenerationConfig.ResponseModalities)
		}

		// Check for reference image in request
		if len(req.Contents[0].Parts) < 2 {
			t.Error("expected reference image in parts")
		}

		resp := generateResponse{
			Candidates: []struct {
				Content struct {
					Parts []responsePart `json:"parts"`
				} `json:"content"`
			}{
				{Content: struct {
					Parts []responsePart `json:"parts"`
				}{Parts: []responsePart{
					{Text: "here's your image"},
					{InlineData: &InlineData{MimeType: "image/png", Data: fakeImageB64}},
				}}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := &Client{
		APIKey: "test-key",
		HTTPClient: &http.Client{
			Transport: &redirectTransport{target: server.URL, inner: http.DefaultTransport},
		},
	}

	resp, err := client.GenerateImage("test-model", "draw something", []byte("ref-photo"), "image/png", "3:2")
	if err != nil {
		t.Fatalf("GenerateImage() error: %v", err)
	}
	if resp.Text != "here's your image" {
		t.Errorf("Text = %q, want %q", resp.Text, "here's your image")
	}
	if len(resp.ImageData) != len(fakeImage) {
		t.Errorf("ImageData length = %d, want %d", len(resp.ImageData), len(fakeImage))
	}
	if resp.MimeType != "image/png" {
		t.Errorf("MimeType = %q, want %q", resp.MimeType, "image/png")
	}
}

func TestGenerateText_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"code":429,"message":"rate limited"}}`))
	}))
	defer server.Close()

	client := &Client{
		APIKey: "test-key",
		HTTPClient: &http.Client{
			Transport: &redirectTransport{target: server.URL, inner: http.DefaultTransport},
		},
	}

	_, err := client.GenerateText("test-model", "hello")
	if err == nil {
		t.Error("expected error for 429 response")
	}
}

func TestGenerateImage_NoImageInResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := generateResponse{
			Candidates: []struct {
				Content struct {
					Parts []responsePart `json:"parts"`
				} `json:"content"`
			}{
				{Content: struct {
					Parts []responsePart `json:"parts"`
				}{Parts: []responsePart{{Text: "no image for you"}}}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := &Client{
		APIKey: "test-key",
		HTTPClient: &http.Client{
			Transport: &redirectTransport{target: server.URL, inner: http.DefaultTransport},
		},
	}

	_, err := client.GenerateImage("test-model", "draw", nil, "", "")
	if err == nil {
		t.Error("expected error when no image in response")
	}
}

// redirectTransport rewrites requests to point at a test server.
type redirectTransport struct {
	target string
	inner  http.RoundTripper
}

func (t *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	req.URL.Host = t.target[len("http://"):]
	return t.inner.RoundTrip(req)
}
