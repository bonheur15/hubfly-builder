package dockerfileparams

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

var dockerfileIdentifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func HasParams(args, env map[string]string) bool {
	return len(args) > 0 || len(env) > 0
}

func BuildArgs(base, args, env map[string]string) map[string]string {
	if len(args) == 0 && len(env) == 0 {
		return base
	}

	out := make(map[string]string, len(base)+len(args)+len(env))
	for key, value := range base {
		out[key] = value
	}
	for key, value := range args {
		out[key] = value
	}
	for key, value := range env {
		out[key] = value
	}
	return out
}

func Stage(path string, args, env map[string]string) ([]byte, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !HasParams(args, env) {
		return content, nil
	}

	if err := validateKeys("dockerfileArgs", args); err != nil {
		return nil, err
	}
	if err := validateKeys("dockerfileEnv", env); err != nil {
		return nil, err
	}

	staged := inject(string(content), args, env)
	if err := os.WriteFile(path, []byte(staged), 0644); err != nil {
		return nil, err
	}
	return []byte(staged), nil
}

func validateKeys(field string, values map[string]string) error {
	for key := range values {
		if !dockerfileIdentifierPattern.MatchString(key) {
			return fmt.Errorf("%s contains invalid Dockerfile identifier %q", field, key)
		}
	}
	return nil
}

func inject(content string, args, env map[string]string) string {
	lines := strings.SplitAfter(content, "\n")
	insertAt := parserDirectiveEnd(lines)

	var builder strings.Builder
	for i, line := range lines {
		if i == insertAt {
			writeGlobalArgs(&builder, args, env)
		}
		builder.WriteString(line)
		if isFromLine(line) {
			writeStageParams(&builder, args, env)
		}
	}
	if insertAt >= len(lines) {
		writeGlobalArgs(&builder, args, env)
	}
	return builder.String()
}

func parserDirectiveEnd(lines []string) int {
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "# syntax=") ||
			strings.HasPrefix(lower, "# escape=") ||
			strings.HasPrefix(lower, "# check=") {
			continue
		}
		return i
	}
	return len(lines)
}

func isFromLine(line string) bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(line)), "FROM ")
}

func writeGlobalArgs(builder *strings.Builder, args, env map[string]string) {
	keys := sortedUnionKeys(args, env)
	if len(keys) == 0 {
		return
	}
	builder.WriteString("\n# Hubfly request Dockerfile args/env\n")
	for _, key := range keys {
		fmt.Fprintf(builder, "ARG %s\n", key)
	}
	builder.WriteString("\n")
}

func writeStageParams(builder *strings.Builder, args, env map[string]string) {
	argKeys := sortedMapKeys(args)
	envKeys := sortedMapKeys(env)
	if len(argKeys) == 0 && len(envKeys) == 0 {
		return
	}

	builder.WriteString("\n# Hubfly request Dockerfile args/env\n")
	for _, key := range argKeys {
		fmt.Fprintf(builder, "ARG %s\n", key)
	}
	for _, key := range envKeys {
		fmt.Fprintf(builder, "ARG %s\n", key)
		fmt.Fprintf(builder, "ENV %s=${%s}\n", key, key)
	}
	builder.WriteString("\n")
}

func sortedUnionKeys(first, second map[string]string) []string {
	seen := make(map[string]struct{}, len(first)+len(second))
	for key := range first {
		seen[key] = struct{}{}
	}
	for key := range second {
		seen[key] = struct{}{}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
