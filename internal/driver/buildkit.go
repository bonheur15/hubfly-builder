package driver

import (
	"fmt"
	"os/exec"
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

type BuildOpts struct {
	ContextPath    string
	Dockerfileath string
	ImageTag       string
}

func (bk *BuildKit) BuildCommand(opts BuildOpts) *exec.Cmd {
	// Example: buildctl build --frontend dockerfile.v0 --local context=. --local dockerfile=. --output type=image,name=my-image,push=true
	args := []string{
		"build",
		"--addr", bk.Addr,
		"--frontend", "dockerfile.v0",
		"--local", fmt.Sprintf("context=%s", opts.ContextPath),
		"--local", fmt.Sprintf("dockerfile=%s", opts.Dockerfileath),
		"--output", fmt.Sprintf("type=image,name=%s,push=true", opts.ImageTag),
	}
	return exec.Command("buildctl", args...)
}