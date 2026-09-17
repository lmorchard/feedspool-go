package topics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/lmorchard/feedspool-go/internal/config"
	"github.com/lmorchard/feedspool-go/internal/httpclient"
)

const (
	emptyTopicLabel   = "Empty Topic"
	unknownTopicLabel = "Unknown Topic"
	hdrContentType    = "Content-Type"
	appJSON           = "application/json"
)

// Labeler defines how we ask an LLM to label a cluster of text items.
type Labeler interface {
	LabelCluster(ctx context.Context, titles []string) (string, error)
	ModelID() string
}

type generateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

type generateResponse struct {
	Response string `json:"response"`
}

type OllamaLabeler struct {
	client  *httpclient.Client
	baseURL string
	model   string
	apiKey  string
}

func NewOllamaLabeler(cfg config.TopicsConfig, client *httpclient.Client) *OllamaLabeler {
	return &OllamaLabeler{
		client:  client,
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		model:   cfg.Model,
		apiKey:  cfg.APIKey,
	}
}

func (l *OllamaLabeler) ModelID() string {
	return l.model
}

func (l *OllamaLabeler) LabelCluster(ctx context.Context, titles []string) (string, error) {
	if len(titles) == 0 {
		return emptyTopicLabel, nil
	}

	prompt := "Here are snippets from a single news topic today. Provide a concise 3-5 word label " +
		"for this topic. Output ONLY the label.\n\n"
	for i, t := range titles {
		prompt += fmt.Sprintf("Item %d:\n%s\n\n", i+1, t)
	}

	body, err := json.Marshal(generateRequest{
		Model:  l.model,
		Prompt: prompt,
		Stream: false,
	})
	if err != nil {
		return "", fmt.Errorf("failed to encode generate request: %w", err)
	}

	headers := map[string]string{hdrContentType: appJSON}
	if l.apiKey != "" {
		headers["Authorization"] = "Bearer " + l.apiKey
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := l.client.Do(&httpclient.Request{
		URL:     l.baseURL + "/api/generate",
		Method:  http.MethodPost,
		Headers: headers,
		Body:    bytes.NewReader(body),
		Context: ctx,
	})
	if err != nil {
		return "", fmt.Errorf("failed to reach LLM provider at %s: %w", l.baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("LLM provider returned HTTP %d", resp.StatusCode)
	}

	var decoded generateResponse
	if err := json.NewDecoder(resp.BodyReader).Decode(&decoded); err != nil {
		return "", fmt.Errorf("failed to decode generation response: %w", err)
	}

	label := strings.TrimSpace(decoded.Response)
	label = strings.Trim(label, `"`) // sometimes LLMs wrap the output in quotes
	if label == "" {
		return unknownTopicLabel, nil
	}

	return label, nil
}
