package tui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/chris/llm-router/internal/proxy"
)

type StatsResponse struct {
	TotalRequests  int                    `json:"total_requests"`
	Successes      int                    `json:"successes"`
	Failures       int                    `json:"failures"`
	InputTokens    int                    `json:"input_tokens"`
	OutputTokens   int                    `json:"output_tokens"`
	CachedTokens   int                    `json:"cached_tokens"`
	ByVirtualModel map[string]ModelStat   `json:"by_virtual_model"`
	ByProvider     map[string]ModelStat   `json:"by_provider"`
}

type ModelStat struct {
	Requests     int `json:"requests"`
	Successes    int `json:"successes"`
	Failures     int `json:"failures"`
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	CachedTokens int `json:"cached_tokens"`
}

type APIClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewAPIClient(baseURL, apiKey string) *APIClient {
	return &APIClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		httpClient: &http.Client{},
	}
}

func (c *APIClient) GetStats() (*StatsResponse, error) {
	req, err := http.NewRequest("GET", c.baseURL+"/api/v1/stats", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connect to proxy: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("stats returned %d", resp.StatusCode)
	}

	var stats StatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return nil, err
	}

	return &stats, nil
}

func (c *APIClient) StreamLogs(ch chan<- proxy.RequestLog) error {
	req, err := http.NewRequest("GET", c.baseURL+"/api/v1/stats/logs/stream", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("connect to log stream: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("log stream returned %d", resp.StatusCode)
	}

	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		data = strings.TrimSpace(data)

		var log proxy.RequestLog
		if err := json.Unmarshal([]byte(data), &log); err != nil {
			continue
		}

		ch <- log
	}
}
