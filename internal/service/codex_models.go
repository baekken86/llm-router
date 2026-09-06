package service

import (
	"context"
	"errors"
	"log/slog"
	"strconv"

	"github.com/chris/llm-router/internal/models"
)

// CodexFallbackModels is the static codex model seed (design §3.3). Discovery
// falls back to it whenever the live catalog is unavailable so a codex
// provider is never worse than the claude-code status quo (design §4.7).
// Exported so the CLI's predefined-models flow (cmd/llm-router) can reuse it.
var CodexFallbackModels = []string{
	"gpt-5.1",
	"gpt-5.1-codex",
	"gpt-5.1-codex-max",
	"gpt-5.1-codex-mini",
	"gpt-5",
	"gpt-5-codex",
	"gpt-5-codex-mini",
}

// ErrCodexUnauthorized marks an HTTP 401 from the codex catalog. The lister
// seam (CodexModelLister) must wrap its auth failures with this sentinel so
// discovery can attempt one token refresh and retry. The cmd-side adapter
// translates proxy.ProviderError{StatusCode: 401} into it.
var ErrCodexUnauthorized = errors.New("codex catalog: unauthorized")

// codexDiscoveryBaseURLFallback is used when a codex provider was created
// without the canonical ChatGPT backend base URL.
const codexDiscoveryBaseURLFallback = "https://chatgpt.com/backend-api/codex"

// oauthTokenSource is the slice of OAuthService codex discovery needs: resolve
// a valid access token (refreshing when the rotation policy allows).
type oauthTokenSource interface {
	GetValidToken(ctx context.Context, providerID int64) (string, error)
}

// oauthRowSource is the slice of OAuthRepository codex discovery needs: read
// the stored token row for the ChatGPT account id.
type oauthRowSource interface {
	GetByProviderID(ctx context.Context, providerID int64) (*models.OAuthToken, error)
}

// CodexReasoningLevel is one supported reasoning effort of a codex model.
type CodexReasoningLevel struct {
	Effort      string `json:"effort"`
	Description string `json:"description"`
}

// CodexModelInfo is one entry of the ChatGPT codex model catalog as seen by
// the model service. It mirrors proxy.CodexModelInfo; internal/proxy imports
// internal/service, so the service cannot reference the proxy type directly —
// the cmd-side adapter converts between them. SupportedInAPI is a pointer so
// the spec's "supported_in_api != false" rule (§3.4) keeps omitted fields
// passing.
type CodexModelInfo struct {
	Slug                     string                `json:"slug"`
	DisplayName              string                `json:"display_name"`
	Description              string                `json:"description"`
	DefaultReasoningLevel    string                `json:"default_reasoning_level"`
	SupportedReasoningLevels []CodexReasoningLevel `json:"supported_reasoning_levels"`
	Visibility               string                `json:"visibility"`
	SupportedInAPI           *bool                 `json:"supported_in_api"`
	ContextWindow            int                   `json:"context_window"`
	Priority                 int                   `json:"priority"`
}

// CodexModelLister fetches the live codex model catalog (§3.4:
// GET {baseURL}/models?client_version=0.136.0). Implementations must return
// an error wrapping ErrCodexUnauthorized on HTTP 401.
type CodexModelLister interface {
	ListModels(ctx context.Context, baseURL, accessToken, accountID string) ([]CodexModelInfo, error)
}

// SetCodexDiscovery wires the collaborators codex model discovery needs. It is
// a free function (not part of ModelService) because the wiring happens after
// the OAuth service exists — setup.go and the CLI construct the model service
// before that. Safe to call once during startup; a nil tokens/rows/lister
// downgrades codex discovery to the static fallback list.
func SetCodexDiscovery(ms ModelService, tokens oauthTokenSource, rows oauthRowSource, lister CodexModelLister) {
	s, ok := ms.(*modelService)
	if !ok {
		panic("SetCodexDiscovery: unexpected ModelService implementation")
	}
	s.codexTokens = tokens
	s.codexRows = rows
	s.codexLister = lister
}

// codexAccountID reads the ChatGPT account id from the provider's oauth row
// ("" when unknown — the header is simply omitted upstream).
func (s *modelService) codexAccountID(ctx context.Context, providerID int64) string {
	if s.codexRows == nil {
		return ""
	}
	row, err := s.codexRows.GetByProviderID(ctx, providerID)
	if err != nil || row == nil {
		return ""
	}
	return row.AccountID
}

// codexFallbackModels wraps CodexFallbackModels as discovery results with a
// warning explaining why the live catalog was not used.
func codexFallbackModels(reason string) []discoveredModel {
	slog.Warn("codex model discovery failed; using static seed list", "reason", reason)
	out := make([]discoveredModel, 0, len(CodexFallbackModels))
	for _, name := range CodexFallbackModels {
		out = append(out, discoveredModel{name: name})
	}
	return out
}

// codexBaseURL normalizes the provider base URL for catalog requests.
func codexBaseURL(provider *models.Provider) string {
	base := provider.BaseURL
	if base == "" {
		base = codexDiscoveryBaseURLFallback
	}
	return base
}

// fetchCodexModels discovers the live codex catalog (design §4.7). It never
// fails: on any error it falls back to the static seed list. On a 401 it
// re-resolves the token (one synchronous refresh when the policy allows) and
// retries once.
func (s *modelService) fetchCodexModels(ctx context.Context, provider *models.Provider) []discoveredModel {
	if s.codexLister == nil || s.codexTokens == nil || s.codexRows == nil {
		return codexFallbackModels("codex discovery collaborators not wired")
	}

	token, err := s.codexTokens.GetValidToken(ctx, provider.ID)
	if err != nil || token == "" {
		reason := "no oauth token"
		if err != nil {
			reason = "oauth token unavailable: " + err.Error()
		}
		return codexFallbackModels(reason)
	}

	accountID := s.codexAccountID(ctx, provider.ID)
	baseURL := codexBaseURL(provider)

	infos, err := s.codexLister.ListModels(ctx, baseURL, token, accountID)
	if err != nil && errors.Is(err, ErrCodexUnauthorized) {
		// One synchronous token refresh → single retry (design §4.7). When the
		// OAuthService re-resolves a rotated token the retry succeeds; a policy
		// that still considers the token valid returns the same one and the
		// retry merely re-confirms the 401 before the fallback.
		if refreshed, rerr := s.codexTokens.GetValidToken(ctx, provider.ID); rerr == nil && refreshed != "" {
			token = refreshed
			infos, err = s.codexLister.ListModels(ctx, baseURL, token, accountID)
		}
	}
	if err != nil {
		return codexFallbackModels(err.Error())
	}

	out := make([]discoveredModel, 0, len(infos))
	for _, info := range infos {
		// Spec §3.4: keep visibility == "list" and supported_in_api != false
		// (omitted field counts as supported).
		if info.Visibility != "list" {
			continue
		}
		if info.SupportedInAPI != nil && !*info.SupportedInAPI {
			continue
		}
		if info.Slug == "" {
			continue
		}

		dm := discoveredModel{name: info.Slug}
		tags := map[string]string{}
		if info.ContextWindow > 0 {
			// context_window matches the existing tag key in data/models.json,
			// import_service.go and the TUI/web abbreviations.
			tags["context_window"] = strconv.Itoa(info.ContextWindow)
		}
		if info.DefaultReasoningLevel != "" {
			tags["default_reasoning_level"] = info.DefaultReasoningLevel
		}
		if len(tags) > 0 {
			dm.tags = tags
		}
		out = append(out, dm)
	}

	if len(out) == 0 {
		return codexFallbackModels("catalog listed no models")
	}
	return out
}
