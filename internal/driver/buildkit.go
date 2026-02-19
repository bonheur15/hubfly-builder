package driver

import (
	"fmt"
	"os/exec"
	"sort"
)

type BuildKit struct {
	// buildkitd address, e.g., "unix:///run/buildkit/buildkitd.sock"
	// This can be configured via startup flags.
	Addr string
}

func NewBuildKit(addr string) *BuildKit {
	if addr == "" {
		// Provide a default, but it's better to configure this.
		addr = "unix:///run/buildkit/buildkitd.sock"
	}
	return &BuildKit{Addr: addr}
}

type BuildSecret struct {
	ID  string
	Src string
}

type BuildOpts struct {
	ContextPath    string
	DockerfilePath string
	ImageTag       string
	BuildArgs      map[string]string
	Secrets        []BuildSecret
}

func (bk *BuildKit) BuildCommand(opts BuildOpts) *exec.Cmd {
	// Example: buildctl --addr <addr> build --frontend dockerfile.v0 --local context=. --local dockerfile=. --output type=image,name=my-image,push=true
	args := []string{
		"--addr", bk.Addr,
		"build",
		"--progress=plain",
		"--frontend", "dockerfile.v0",
		"--local", fmt.Sprintf("context=%s", opts.ContextPath),
		"--local", fmt.Sprintf("dockerfile=%s", opts.DockerfilePath),
	}

	for _, key := range sortedMapKeys(opts.BuildArgs) {
		args = append(args, "--opt", fmt.Sprintf("build-arg:%s=%s", key, opts.BuildArgs[key]))
	}

	for _, secret := range sortedSecrets(opts.Secrets) {
		args = append(args, "--secret", fmt.Sprintf("id=%s,src=%s", secret.ID, secret.Src))
	}

	args = append(args, "--output", fmt.Sprintf("type=image,name=%s,push=true,registry.insecure=true", opts.ImageTag))
	return exec.Command("buildctl", args...)
}

func sortedMapKeys(values map[string]string) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedSecrets(secrets []BuildSecret) []BuildSecret {
	if len(secrets) == 0 {
		return nil
	}
	out := make([]BuildSecret, len(secrets))
	copy(out, secrets)
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID == out[j].ID {
			return out[i].Src < out[j].Src
		}
		return out[i].ID < out[j].ID
	})
	return out
}
