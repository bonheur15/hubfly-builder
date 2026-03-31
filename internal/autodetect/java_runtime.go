package autodetect

import "strings"

func javaSelectJarCommand() string {
	command := `set -e; jar=""; for candidate in target/quarkus-app/quarkus-run.jar build/quarkus-app/quarkus-run.jar target/*-runner.jar build/*-runner.jar target/*-boot.jar build/libs/*-boot.jar target/*-all.jar build/libs/*-all.jar target/*.jar build/libs/*.jar; do for f in $candidate; do if [ -f "$f" ]; then case "$f" in *-plain.jar) continue ;; esac; jar="$f"; break 2; fi; done; done; if [ -z "$jar" ]; then echo "No runnable jar found"; exit 1; fi; cp "$jar" /app/app.jar`
	return strings.TrimSpace(command)
}
