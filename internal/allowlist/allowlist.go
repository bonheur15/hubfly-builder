package allowlist

import (
	"encoding/json"
	"os"
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
	for _, a := range allowed {
		if strings.TrimSpace(a) == strings.TrimSpace(cmd) {
			return true
		}
	}
	return false
}
