package driver

import (
	"strings"
	"testing"
)

func TestBuildCommandExportsDockerArchiveToHost(t *testing.T) {
	bk := NewBuildKit("tcp://10.0.0.2:1234")
	cmd := bk.BuildCommand(BuildOpts{
		ContextPath:    "/tmp/context",
		DockerfilePath: "/tmp/context",
		ImageTag:       "127.0.0.1:10009/user/project:tag",
		ExportPath:     "/tmp/image.tar",
		BuildArgs: map[string]string{
			"FOO": "bar",
		},
		Secrets: []BuildSecret{
			{ID: "TOKEN", Src: "/tmp/token"},
		},
	})

	got := strings.Join(cmd.Args, " ")
	for _, want := range []string{
		"buildctl",
		"--addr tcp://10.0.0.2:1234",
		"--output type=docker,name=127.0.0.1:10009/user/project:tag,dest=/tmp/image.tar",
		"--opt build-arg:FOO=bar",
		"--secret id=TOKEN,src=/tmp/token",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in command, got %q", want, got)
		}
	}

	if strings.Contains(got, "push=true") {
		t.Fatalf("did not expect direct registry push output in command: %q", got)
	}
}
