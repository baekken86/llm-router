package proxy

import (
	"log/slog"
	"regexp"
	"strings"
	"sync"
)

type CavemanInterceptor struct {
	enabled bool
	logger  *slog.Logger
	mu      sync.RWMutex
	stats   CavemanStats
}

type CavemanStats struct {
	Interceptions   int
	OriginalTokens  int
	CompressedTokens int
}

func NewCavemanInterceptor(logger *slog.Logger) *CavemanInterceptor {
	return &CavemanInterceptor{
		enabled: true,
		logger:  logger,
	}
}

func (c *CavemanInterceptor) SetEnabled(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.enabled = enabled
}

func (c *CavemanInterceptor) IsEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.enabled
}

func (c *CavemanInterceptor) GetStats() CavemanStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stats
}

func (c *CavemanInterceptor) InterceptOutput(text string) (string, bool) {
	if !c.IsEnabled() || text == "" {
		return text, false
	}

	compressed := c.compress(text)
	if compressed == text {
		return text, false
	}

	originalTokens := estimateTokens(text)
	compressedTokens := estimateTokens(compressed)

	c.mu.Lock()
	c.stats.Interceptions++
	c.stats.OriginalTokens += originalTokens
	c.stats.CompressedTokens += compressedTokens
	c.mu.Unlock()

	c.logger.Debug("caveman intercepted",
		"original_tokens", originalTokens,
		"compressed_tokens", compressedTokens,
		"saved", originalTokens-compressedTokens,
	)

	return compressed, true
}

func (c *CavemanInterceptor) compress(text string) string {
	result := text

	result = c.removeRedundantPhrases(result)
	result = c.compactCodeBlocks(result)
	result = c.removeFillerWords(result)
	result = c.compactLists(result)
	result = c.compactExplanations(result)

	return result
}

func (c *CavemanInterceptor) removeRedundantPhrases(text string) string {
	patterns := []struct {
		re   *regexp.Regexp
		rep  string
	}{
		{regexp.MustCompile(`(?i)\bhere's (a |an |the )?`), ""},
		{regexp.MustCompile(`(?i)\blet me (help you |)?`), ""},
		{regexp.MustCompile(`(?i)\bI'll (go ahead and |)?`), ""},
		{regexp.MustCompile(`(?i)\bnow,?\s+`), ""},
		{regexp.MustCompile(`(?i)\bso,?\s+`), ""},
		{regexp.MustCompile(`(?i)\bwell,?\s+`), ""},
		{regexp.MustCompile(`(?i)\bokay,?\s+`), ""},
		{regexp.MustCompile(`(?i)\balright,?\s+`), ""},
		{regexp.MustCompile(`(?i)\bto (summarize|summarise),?\s+`), ""},
		{regexp.MustCompile(`(?i)\bin summary,?\s+`), ""},
		{regexp.MustCompile(`(?i)\bas (I |we )?(mentioned|said|noted),?\s+`), ""},
		{regexp.MustCompile(`(?i)\bbasically,?\s+`), ""},
		{regexp.MustCompile(`(?i)\bliterally,?\s+`), ""},
		{regexp.MustCompile(`(?i)\bessentially,?\s+`), ""},
		{regexp.MustCompile(`(?i)\bjust (a |an )?`), ""},
		{regexp.MustCompile(`(?i)\bthe (following|below)\s+`), ""},
		{regexp.MustCompile(`(?i)\bthis (is a |is an )?`), ""},
		{regexp.MustCompile(`(?i)\byou can (use|see|find|check)\b`), ""},
		{regexp.MustCompile(`(?i)\bnote that\b`), ""},
		{regexp.MustCompile(`(?i)\bplease note\b`), ""},
		{regexp.MustCompile(`(?i)\bit's (worth|important|notable) (noting|mentioning|pointing out) that\b`), ""},
		{regexp.MustCompile(`(?i)\bthe (key|main|important) (point|thing|idea) (is|here is)\b`), ""},
	}

	for _, p := range patterns {
		result := p.rep
		text = p.re.ReplaceAllString(text, result)
	}

	return strings.TrimSpace(text)
}

func (c *CavemanInterceptor) compactCodeBlocks(text string) string {
	re := regexp.MustCompile(`(?s)` + "```" + `(\w+)?\n(.*?)` + "```")
	return re.ReplaceAllStringFunc(text, func(match string) string {
		parts := re.FindStringSubmatch(match)
		if len(parts) < 3 {
			return match
		}
		lang := parts[1]
		code := strings.TrimSpace(parts[2])

		code = c.removeCodeComments(code)
		code = c.compactCodeSpacing(code)

		if lang != "" {
			return "```" + lang + "\n" + code + "```"
		}
		return "```\n" + code + "```"
	})
}

func (c *CavemanInterceptor) removeCodeComments(code string) string {
	lines := strings.Split(code, "\n")
	var result []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "*") {
			if !strings.Contains(trimmed, "TODO") && !strings.Contains(trimmed, "FIXME") && !strings.Contains(trimmed, "HACK") && !strings.Contains(trimmed, "NOTE") {
				continue
			}
		}

		if strings.HasPrefix(trimmed, "import (") || strings.HasPrefix(trimmed, "from (") {
			result = append(result, line)
			continue
		}

		result = append(result, line)
	}

	return strings.Join(result, "\n")
}

func (c *CavemanInterceptor) compactCodeSpacing(code string) string {
	lines := strings.Split(code, "\n")
	var result []string
	prevEmpty := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if !prevEmpty {
				result = append(result, "")
			}
			prevEmpty = true
		} else {
			result = append(result, line)
			prevEmpty = false
		}
	}

	return strings.Join(result, "\n")
}

func (c *CavemanInterceptor) removeFillerWords(text string) string {
	fillers := []string{
		`\bactually\b`, `\bbasically\b`, `\bliterally\b`, `\bessentially\b`,
		`\bprobably\b`, `\bmaybe\b`, `\bperhaps\b`, `\bsort of\b`, `\bkind of\b`,
		`\bI think\b`, `\bI believe\b`, `\bin my opinion\b`, `\byou know\b`,
		`\blike\b`, `\bso\b`, `\bwell\b`, `\bokay\b`, `\balright\b`,
		`\bright\b`, `\byeah\b`, `\byup\b`, `\byep\b`,
	}

	for _, filler := range fillers {
		re := regexp.MustCompile(filler)
		text = re.ReplaceAllString(text, "")
	}

	text = regexp.MustCompile(`[ \t]{2,}`).ReplaceAllString(text, " ")
	text = regexp.MustCompile(`\n{3,}`).ReplaceAllString(text, "\n\n")

	return strings.TrimSpace(text)
}

func (c *CavemanInterceptor) compactLists(text string) string {
	lines := strings.Split(text, "\n")
	var result []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") || strings.HasPrefix(trimmed, "• ") {
			content := trimmed[2:]
			content = regexp.MustCompile(`(?i)\bthat is\b`).ReplaceAllString(content, "")
			content = regexp.MustCompile(`(?i)\bwhich is\b`).ReplaceAllString(content, "")
			content = regexp.MustCompile(`(?i)\bthis means\b`).ReplaceAllString(content, "")
			result = append(result, "- "+strings.TrimSpace(content))
		} else {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

func (c *CavemanInterceptor) compactExplanations(text string) string {
	text = regexp.MustCompile(`(?i)\bfor example\b`).ReplaceAllString(text, "e.g.,")
	text = regexp.MustCompile(`(?i)\bfor instance\b`).ReplaceAllString(text, "e.g.,")
	text = regexp.MustCompile(`(?i)\bin order to\b`).ReplaceAllString(text, "to")
	text = regexp.MustCompile(`(?i)\bdue to the fact that\b`).ReplaceAllString(text, "because")
	text = regexp.MustCompile(`(?i)\bat this point in time\b`).ReplaceAllString(text, "now")
	text = regexp.MustCompile(`(?i)\bin the event that\b`).ReplaceAllString(text, "if")
	text = regexp.MustCompile(`(?i)\bfor the purpose of\b`).ReplaceAllString(text, "to")
	text = regexp.MustCompile(`(?i)\bin the process of\b`).ReplaceAllString(text, "")
	text = regexp.MustCompile(`(?i)\bwith regard to\b`).ReplaceAllString(text, "about")
	text = regexp.MustCompile(`(?i)\bin terms of\b`).ReplaceAllString(text, "for")
	text = regexp.MustCompile(`(?i)\bon a (daily|regular|weekly) basis\b`).ReplaceAllString(text, "daily/regularly/weekly")
	text = regexp.MustCompile(`(?i)\bat the present time\b`).ReplaceAllString(text, "now")
	text = regexp.MustCompile(`(?i)\bprior to\b`).ReplaceAllString(text, "before")
	text = regexp.MustCompile(`(?i)\bsubsequent to\b`).ReplaceAllString(text, "after")

	return strings.TrimSpace(text)
}
