package proxy

import (
	"bytes"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
)

type RTKInterceptor struct {
	binaryPath string
	enabled    bool
	logger     *slog.Logger
	mu         sync.RWMutex
	stats      RTKStats
}

type RTKStats struct {
	Interceptions   int
	OriginalTokens  int
	CompressedTokens int
}

func NewRTKInterceptor(logger *slog.Logger) *RTKInterceptor {
	path, err := exec.LookPath("rtk")
	enabled := err == nil

	if enabled {
		logger.Info("rtk found", "path", path)
	} else {
		logger.Info("rtk not found, interception disabled")
	}

	return &RTKInterceptor{
		binaryPath: path,
		enabled:    enabled,
		logger:     logger,
	}
}

func (r *RTKInterceptor) SetEnabled(enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.enabled = enabled
}

func (r *RTKInterceptor) IsEnabled() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.enabled && r.binaryPath != ""
}

func (r *RTKInterceptor) GetStats() RTKStats {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.stats
}

func (r *RTKInterceptor) InterceptToolResult(toolName string, toolInput string, output string) (string, bool) {
	if !r.IsEnabled() {
		return output, false
	}

	if !isBashCommand(toolName) {
		return output, false
	}

	cmd := r.buildRTKCommand(toolName, toolInput)
	if cmd == "" {
		return output, false
	}

	compressed, err := r.runRTK(cmd, output)
	if err != nil {
		r.logger.Debug("rtk interception failed", "error", err)
		return output, false
	}

	originalTokens := estimateTokens(output)
	compressedTokens := estimateTokens(compressed)

	r.mu.Lock()
	r.stats.Interceptions++
	r.stats.OriginalTokens += originalTokens
	r.stats.CompressedTokens += compressedTokens
	r.mu.Unlock()

	r.logger.Debug("rtk intercepted",
		"tool", toolName,
		"original_tokens", originalTokens,
		"compressed_tokens", compressedTokens,
		"saved", originalTokens-compressedTokens,
	)

	return compressed, true
}

func (r *RTKInterceptor) buildRTKCommand(toolName string, toolInput string) string {
	input := strings.TrimSpace(toolName)

	switch {
	case strings.HasPrefix(input, "bash"):
		return extractBashCommand(toolInput)
	case strings.HasPrefix(input, "shell"):
		return extractBashCommand(toolInput)
	case input == "execute" || input == "run":
		return extractBashCommand(toolInput)
	default:
		return ""
	}
}

func extractBashCommand(input string) string {
	input = strings.TrimSpace(input)

	if idx := strings.Index(input, "command="); idx >= 0 {
		rest := input[idx+8:]
		if len(rest) > 0 && rest[0] == '"' {
			if end := strings.Index(rest[1:], "\""); end >= 0 {
				return rest[1 : end+1]
			}
		}
		return strings.Fields(rest)[0]
	}

	fields := strings.Fields(input)
	if len(fields) > 0 {
		return fields[0]
	}

	return ""
}

func isBashCommand(toolName string) bool {
	name := strings.ToLower(strings.TrimSpace(toolName))
	return name == "bash" || name == "shell" || name == "execute" || name == "run"
}

func (r *RTKInterceptor) runRTK(command string, output string) (string, error) {
	args := []string{"--filter-raw", command}
	cmd := exec.Command(r.binaryPath, args...)
	cmd.Stdin = strings.NewReader(output)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", err
	}

	return stdout.String(), nil
}

func estimateTokens(text string) int {
	return len(text) / 4
}
