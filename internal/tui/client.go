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

type VirtualModelResponse struct {
	ID         int64       `json:"id"`
	Name       string      `json:"name"`
	FilterExpr interface{} `json:"filter_expr"`
	SortExpr   interface{} `json:"sort_expr"`
}

type ResolvedModelResponse struct {
	Position     int               `json:"position"`
	ModelID      int64             `json:"model_id"`
	ModelName    string            `json:"model_name"`
	ProviderID   int64             `json:"provider_id"`
	ProviderName string            `json:"provider_name"`
	APIType      string            `json:"api_type"`
	Tags         map[string]string `json:"tags"`
}

type ResolvedResponse struct {
	VirtualModel string                 `json:"virtual_model"`
	Filter       string                 `json:"filter"`
	Sort         string                 `json:"sort"`
	Models       []ResolvedModelResponse `json:"models"`
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

func (c *APIClient) doGET(path string, target interface{}) error {
	req, err := http.NewRequest("GET", c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("GET %s returned %d", path, resp.StatusCode)
	}

	return json.NewDecoder(resp.Body).Decode(target)
}

func (c *APIClient) GetStats() (*StatsResponse, error) {
	var stats StatsResponse
	if err := c.doGET("/api/v1/stats", &stats); err != nil {
		return nil, err
	}
	return &stats, nil
}

func (c *APIClient) GetVirtualModels() ([]VirtualModelResponse, error) {
	var vms []VirtualModelResponse
	if err := c.doGET("/api/v1/virtual-models", &vms); err != nil {
		return nil, err
	}
	return vms, nil
}

func (c *APIClient) GetResolvedModels(vmID int64) ([]ResolvedModelResponse, error) {
	var resp ResolvedResponse
	if err := c.doGET(fmt.Sprintf("/api/v1/virtual-models/%d/resolved", vmID), &resp); err != nil {
		return nil, err
	}
	return resp.Models, nil
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
