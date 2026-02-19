package envplan

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"hubfly-builder/internal/storage"
)

const maxHintFileSize = 1 << 20 // 1 MiB

type Result struct {
	BuildArgs    map[string]string
	BuildSecrets map[string]string
	Entries      []storage.ResolvedEnvVar
}

func (r Result) BuildArgKeys() []string {
	keys := make([]string, 0, len(r.BuildArgs))
	for key := range r.BuildArgs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (r Result) BuildSecretKeys() []string {
	keys := make([]string, 0, len(r.BuildSecrets))
	for key := range r.BuildSecrets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (r Result) RuntimeKeys() []string {
	keys := make([]string, 0)
	for _, entry := range r.Entries {
		if entry.Scope == "runtime" || entry.Scope == "both" {
			keys = append(keys, entry.Key)
		}
	}
	sort.Strings(keys)
	return keys
}

func Resolve(buildContext string, env map[string]string) Result {
	if len(env) == 0 {
		return Result{}
	}

	hints := collectBuildHints(buildContext)

	entries := make([]storage.ResolvedEnvVar, 0, len(env))
	buildArgs := make(map[string]string)
	buildSecrets := make(map[string]string)

	normalizedEnv := make(map[string]string, len(env))
	for key, value := range env {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		normalizedEnv[trimmed] = value
	}

	keys := make([]string, 0, len(normalizedEnv))
	for key := range normalizedEnv {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		value := normalizedEnv[key]

		upperKey := strings.ToUpper(key)
		scope, reason := classifyScope(upperKey, hints)
		secret := classifySecret(upperKey)
		if strings.HasPrefix(reason, "dockerfile-arg") {
			// Dockerfile ARG usage implies the author expects a build-arg value.
			secret = false
		}

		entry := storage.ResolvedEnvVar{
			Key:    key,
			Scope:  scope,
			Secret: secret,
			Reason: reason,
		}
		entries = append(entries, entry)

		if scope == "build" || scope == "both" {
			if secret {
				buildSecrets[key] = value
			} else {
				buildArgs[key] = value
			}
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Key < entries[j].Key
	})

	return Result{
		BuildArgs:    buildArgs,
		BuildSecrets: buildSecrets,
		Entries:      entries,
	}
}

type buildHints struct {
	dockerfileContent string
	configContents    []string
}

func collectBuildHints(buildContext string) buildHints {
	hints := buildHints{}
	dockerfilePath := filepath.Join(buildContext, "Dockerfile")
	hints.dockerfileContent = readUpperFile(dockerfilePath)

	for _, fileName := range buildHintFiles {
		path := filepath.Join(buildContext, fileName)
		content := readUpperFile(path)
		if content != "" {
			hints.configContents = append(hints.configContents, content)
		}
	}

	return hints
}

func readUpperFile(path string) string {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() > maxHintFileSize {
		return ""
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.ToUpper(string(data))
}

func classifyScope(key string, hints buildHints) (string, string) {
	if hasAnyPrefix(key, publicEnvPrefixes) {
		return "both", "public-prefix"
	}

	if reason := buildReferenceReason(key, hints); reason != "" {
		if isRuntimePreferred(key) {
			return "both", reason + "+runtime-signal"
		}
		return "build", reason
	}

	if isRuntimePreferred(key) {
		return "runtime", "runtime-signal"
	}

	return "runtime", "default-runtime"
}

func buildReferenceReason(key string, hints buildHints) string {
	if hints.dockerfileContent != "" {
		if hasDockerfileArg(hints.dockerfileContent, key) {
			return "dockerfile-arg"
		}
		if strings.Contains(hints.dockerfileContent, "$"+key) ||
			strings.Contains(hints.dockerfileContent, "${"+key+"}") {
			return "dockerfile-reference"
		}
	}

	for _, content := range hints.configContents {
		if strings.Contains(content, key) {
			return "build-config-reference"
		}
	}

	return ""
}

func classifySecret(key string) bool {
	if hasAnyPrefix(key, publicEnvPrefixes) {
		return false
	}
	if _, ok := nonSecretKeys[key]; ok {
		return false
	}

	for _, marker := range secretMarkers {
		if strings.Contains(key, marker) {
			return true
		}
	}

	// Unknown keys default to secret.
	return true
}

func isRuntimePreferred(key string) bool {
	if _, ok := runtimePreferredKeys[key]; ok {
		return true
	}
	return hasAnyPrefix(key, runtimePreferredPrefixes)
}

func hasAnyPrefix(key string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

func hasDockerfileArg(content, key string) bool {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ARG ") {
			continue
		}

		decl := strings.TrimSpace(strings.TrimPrefix(line, "ARG "))
		if decl == key || strings.HasPrefix(decl, key+"=") {
			return true
		}
	}
	return false
}

var publicEnvPrefixes = []string{
	"NEXT_PUBLIC_",
	"VITE_",
	"REACT_APP_",
	"NUXT_PUBLIC_",
	"PUBLIC_",
	"EXPO_PUBLIC_",
	"GATSBY_",
	"SVELTEKIT_PUBLIC_",
}

var runtimePreferredKeys = map[string]struct{}{
	"DATABASE_URL": {},
	"REDIS_URL":    {},
	"MONGODB_URI":  {},
	"PORT":         {},
	"NODE_ENV":     {},
	"HOST":         {},
	"TZ":           {},
	"LOG_LEVEL":    {},
}

var runtimePreferredPrefixes = []string{
	"DB_",
	"DATABASE_",
	"REDIS_",
	"POSTGRES_",
	"PG_",
	"MYSQL_",
	"MONGO_",
	"JWT_",
	"SESSION_",
	"COOKIE_",
	"SMTP_",
	"MAIL_",
}

var nonSecretKeys = map[string]struct{}{
	"PORT":      {},
	"NODE_ENV":  {},
	"HOST":      {},
	"TZ":        {},
	"APP_ENV":   {},
	"LOG_LEVEL": {},
}

var secretMarkers = []string{
	"SECRET",
	"TOKEN",
	"PASSWORD",
	"PRIVATE_KEY",
	"API_KEY",
	"ACCESS_KEY",
	"CREDENTIAL",
	"AUTH",
	"CERT",
	"DATABASE_URL",
	"REDIS_URL",
	"CONNECTION_STRING",
}

var buildHintFiles = []string{
	"package.json",
	"bunfig.toml",
	"vite.config.js",
	"vite.config.ts",
	"vite.config.mjs",
	"vite.config.cjs",
	"next.config.js",
	"next.config.ts",
	"next.config.mjs",
	"nuxt.config.js",
	"nuxt.config.ts",
	"webpack.config.js",
	"webpack.config.ts",
	"rollup.config.js",
	"rollup.config.ts",
	"rollup.config.mjs",
	"astro.config.mjs",
	"astro.config.ts",
	"svelte.config.js",
	"svelte.config.ts",
}
