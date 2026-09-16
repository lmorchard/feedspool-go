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

type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatRequest struct {
	Model    string              `json:"model"`
	Messages []openAIChatMessage `json:"messages"`
	Stream   bool                `json:"stream"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

type OpenAILabeler struct {
	client  *httpclient.Client
	baseURL string
	model   string
	apiKey  string
}

func NewOpenAILabeler(cfg config.TopicsConfig, client *httpclient.Client) *OpenAILabeler {
	return &OpenAILabeler{
		client:  client,
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		model:   cfg.Model,
		apiKey:  cfg.APIKey,
	}
}

func (l *OpenAILabeler) ModelID() string {
	return l.model
}

func (l *OpenAILabeler) LabelCluster(ctx context.Context, titles []string) (string, error) {
	if len(titles) == 0 {
		return emptyTopicLabel, nil
	}

	prompt := "Here are snippets from a single news topic today. Provide a concise 3-5 word label " +
		"for this topic. Output ONLY the label.\n\n"
	for i, t := range titles {
		prompt += fmt.Sprintf("Item %d:\n%s\n\n", i+1, t)
	}

	body, err := json.Marshal(openAIChatRequest{
		Model: l.model,
		Messages: []openAIChatMessage{
			{Role: "user", Content: prompt},
		},
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

	url := l.baseURL
	if !strings.HasSuffix(url, "/v1/chat/completions") && !strings.HasSuffix(url, "/chat/completions") {
		// Just in case they provide the root URL instead of the full endpoint
		if strings.HasSuffix(url, "/v1") {
			url += "/chat/completions"
		} else {
			url += "/v1/chat/completions"
		}
	}

	resp, err := l.client.Do(&httpclient.Request{
		URL:     url,
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

	var decoded openAIChatResponse
	if err := json.NewDecoder(resp.BodyReader).Decode(&decoded); err != nil {
		return "", fmt.Errorf("failed to decode generation response: %w", err)
	}

	if len(decoded.Choices) == 0 {
		return unknownTopicLabel, nil
	}

	label := strings.TrimSpace(decoded.Choices[0].Message.Content)
	label = strings.Trim(label, `"`) // sometimes LLMs wrap the output in quotes
	if label == "" {
		return unknownTopicLabel, nil
	}

	return label, nil
}
