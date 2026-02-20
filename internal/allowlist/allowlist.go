package allowlist

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
)

type AllowedCommands struct {
	Prebuild []string `json:"prebuild"`
	Build    []string `json:"build"`
	Run      []string `json:"run"`
}

func LoadAllowedCommands(path string) (*AllowedCommands, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cmds AllowedCommands
	if err := json.Unmarshal(data, &cmds); err != nil {
		return nil, err
	}

	return &cmds, nil
}

func IsCommandAllowed(cmd string, allowed []string) bool {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return false
	}

	for _, a := range allowed {
		pattern := strings.TrimSpace(a)
		if pattern == "" {
			continue
		}
		if pattern == cmd {
			return true
		}
		if strings.Contains(pattern, "*") && wildcardMatch(pattern, cmd) {
			return true
		}
	}
	return false
}

func wildcardMatch(pattern, value string) bool {
	parts := strings.Split(pattern, "*")

	var builder strings.Builder
	builder.WriteString("^")
	for i, part := range parts {
		builder.WriteString(regexp.QuoteMeta(part))
		if i < len(parts)-1 {
			// Keep wildcard matching strict: command tokens can only contain safe chars.
			builder.WriteString("[A-Za-z0-9:._/\\-]+")
		}
	}
	builder.WriteString("$")

	matched, err := regexp.MatchString(builder.String(), value)
	return err == nil && matched
}
