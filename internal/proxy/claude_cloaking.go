package proxy

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	claudeVersion = "2.1.92"
	ccEntrypoint  = "sdk-cli"
	claudeToolSuffix = "_ide"
)

var ccDecoyTools = []map[string]interface{}{
	{"name": "Task", "description": "This tool is currently unavailable.", "input_schema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
	{"name": "TaskOutput", "description": "This tool is currently unavailable.", "input_schema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
	{"name": "TaskStop", "description": "This tool is currently unavailable.", "input_schema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
	{"name": "TaskCreate", "description": "This tool is currently unavailable.", "input_schema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
	{"name": "TaskGet", "description": "This tool is currently unavailable.", "input_schema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
	{"name": "TaskUpdate", "description": "This tool is currently unavailable.", "input_schema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
	{"name": "TaskList", "description": "This tool is currently unavailable.", "input_schema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
	{"name": "Bash", "description": "This tool is currently unavailable.", "input_schema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
	{"name": "Glob", "description": "This tool is currently unavailable.", "input_schema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
	{"name": "Grep", "description": "This tool is currently unavailable.", "input_schema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
	{"name": "Read", "description": "This tool is currently unavailable.", "input_schema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
	{"name": "Edit", "description": "This tool is currently unavailable.", "input_schema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
	{"name": "Write", "description": "This tool is currently unavailable.", "input_schema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
	{"name": "NotebookEdit", "description": "This tool is currently unavailable.", "input_schema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
	{"name": "WebFetch", "description": "This tool is currently unavailable.", "input_schema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
	{"name": "WebSearch", "description": "This tool is currently unavailable.", "input_schema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
	{"name": "AskUserQuestion", "description": "This tool is currently unavailable.", "input_schema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
	{"name": "Skill", "description": "This tool is currently unavailable.", "input_schema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
	{"name": "EnterPlanMode", "description": "This tool is currently unavailable.", "input_schema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
	{"name": "ExitPlanMode", "description": "This tool is currently unavailable.", "input_schema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
}

func generateBillingHeader(payload interface{}) string {
	content, _ := json.Marshal(payload)
	h := sha256.Sum256(content)
	cch := hex.EncodeToString(h[:])[:5]

	buildBytes := make([]byte, 2)
	rand.Read(buildBytes)
	buildHash := hex.EncodeToString(buildBytes)[:3]

	return fmt.Sprintf("x-anthropic-billing-header: cc_version=%s.%s; cc_entrypoint=%s; cch=%s;", claudeVersion, buildHash, ccEntrypoint, cch)
}

func deriveUuid(seed string) string {
	h := sha256.Sum256([]byte(seed))
	hexStr := hex.EncodeToString(h[:])
	return fmt.Sprintf("%s-%s-4%s-%s%s-%s",
		hexStr[0:8],
		hexStr[8:12],
		hexStr[13:16],
		string(hexStr[16]),
		hexStr[17:20],
		hexStr[20:32],
	)
}

func generateFakeUserID(sessionId, apiKey string) string {
	deviceID := sha256Hex("device:" + apiKey)
	accountUuid := deriveUuid("account:" + apiKey)
	sessionUuid := sessionId
	if sessionUuid == "" {
		sessionUuid = randomUUID()
	}
	return fmt.Sprintf(`{"device_id":"%s","account_uuid":"%s","session_id":"%s"}`, deviceID, accountUuid, sessionUuid)
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func randomUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func applyCloaking(body map[string]interface{}, apiKey, sessionId string) map[string]interface{} {
	if !strings.HasPrefix(apiKey, "sk-ant-oat") {
		return body
	}

	result := make(map[string]interface{})
	for k, v := range body {
		result[k] = v
	}

	billingText := generateBillingHeader(body)
	billingBlock := map[string]interface{}{
		"type": "text",
		"text": billingText,
	}

	switch sys := result["system"].(type) {
	case []interface{}:
		if len(sys) > 0 {
			if firstBlock, ok := sys[0].(map[string]interface{}); ok {
				if text, ok := firstBlock["text"].(string); ok && strings.HasPrefix(text, "x-anthropic-billing-header:") {
					break
				}
			}
		}
		result["system"] = append([]interface{}{billingBlock}, sys...)
	case string:
		result["system"] = []interface{}{
			billingBlock,
			map[string]interface{}{"type": "text", "text": sys},
		}
	default:
		result["system"] = []interface{}{billingBlock}
	}

	metadata, _ := result["metadata"].(map[string]interface{})
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	if metadata["user_id"] == nil {
		metadata["user_id"] = generateFakeUserID(sessionId, apiKey)
	}
	result["metadata"] = metadata

	return result
}

func cloakClaudeTools(body map[string]interface{}) map[string]interface{} {
	tools, ok := body["tools"].([]interface{})
	if !ok || len(tools) == 0 {
		return body
	}

	result := make(map[string]interface{})
	for k, v := range body {
		result[k] = v
	}

	var clientTools []interface{}
	for _, t := range tools {
		tool, ok := t.(map[string]interface{})
		if !ok {
			clientTools = append(clientTools, t)
			continue
		}
		if _, hasType := tool["type"]; hasType {
			clientTools = append(clientTools, tool)
			continue
		}
		name, _ := tool["name"].(string)
		tool["name"] = name + claudeToolSuffix
		clientTools = append(clientTools, tool)
	}

	for _, dt := range ccDecoyTools {
		clientTools = append(clientTools, dt)
	}
	result["tools"] = clientTools

	if messages, ok := result["messages"].([]interface{}); ok {
		var renamed []interface{}
		for _, m := range messages {
			msg, ok := m.(map[string]interface{})
			if !ok {
				renamed = append(renamed, m)
				continue
			}
			content, ok := msg["content"].([]interface{})
			if !ok {
				renamed = append(renamed, msg)
				continue
			}
			var renamedContent []interface{}
			for _, c := range content {
				block, ok := c.(map[string]interface{})
				if !ok {
					renamedContent = append(renamedContent, c)
					continue
				}
				if blockType, _ := block["type"].(string); blockType == "tool_use" {
					if name, ok := block["name"].(string); ok {
						block["name"] = name + claudeToolSuffix
					}
				}
				renamedContent = append(renamedContent, block)
			}
			msg["content"] = renamedContent
			renamed = append(renamed, msg)
		}
		result["messages"] = renamed
	}

	return result
}

func decloakToolName(name string) string {
	if strings.HasSuffix(name, claudeToolSuffix) {
		return name[:len(name)-len(claudeToolSuffix)]
	}
	return name
}