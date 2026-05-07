package autodetect

import (
	"path/filepath"
	"strings"
)

func javaSelectJarCommand() string {
	command := `set -e; jar=""; for candidate in target/quarkus-app/quarkus-run.jar build/quarkus-app/quarkus-run.jar target/*-runner.jar build/*-runner.jar target/*-boot.jar build/libs/*-boot.jar target/*-all.jar build/libs/*-all.jar target/*.jar build/libs/*.jar; do for f in $candidate; do if [ -f "$f" ]; then case "$f" in *-plain.jar) continue ;; esac; jar="$f"; break 2; fi; done; done; if [ -z "$jar" ]; then echo "No runnable jar found"; exit 1; fi; cp "$jar" /app/app.jar`
	return strings.TrimSpace(command)
}

func javaSelectQuarkusAppCommand() string {
	command := `set -e; for dir in target/quarkus-app build/quarkus-app; do if [ -f "$dir/quarkus-run.jar" ]; then rm -rf /app/quarkus-app; cp -R "$dir" /app/quarkus-app; exit 0; fi; done; for jar in target/*-runner.jar build/*-runner.jar; do if [ -f "$jar" ]; then parent="$(dirname "$jar")"; rm -rf /app/quarkus-app; mkdir -p /app/quarkus-app; cp "$jar" /app/quarkus-app/quarkus-run.jar; for dir in "$parent/lib" "$parent/app" "$parent/quarkus"; do if [ -d "$dir" ]; then cp -R "$dir" /app/quarkus-app/; fi; done; exit 0; fi; done; echo "No Quarkus runnable jar layout found"; exit 1`
	return strings.TrimSpace(command)
}

func configureJavaRuntimePlan(plan *buildPlan, appPath string) {
	if detectQuarkusProject(appPath) {
		plan.Framework = "quarkus"
		plan.PostBuildCommands = append(plan.PostBuildCommands, javaSelectQuarkusAppCommand())
		plan.RunCommand = "java -jar quarkus-app/quarkus-run.jar"
		return
	}

	if detectSpringBootProject(appPath) {
		plan.Framework = "spring-boot"
	} else if detectMicronautProject(appPath) {
		plan.Framework = "micronaut"
	}
	plan.PostBuildCommands = append(plan.PostBuildCommands, javaSelectJarCommand())
	plan.RunCommand = "java -jar app.jar"
}

func isGradleJavaProject(repoPath string) bool {
	return repoPath != "" && (fileExists(filepath.Join(repoPath, "build.gradle")) || fileExists(filepath.Join(repoPath, "build.gradle.kts")))
}
