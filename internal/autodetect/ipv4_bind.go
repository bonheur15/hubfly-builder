package autodetect

import "strings"

const ipv4AnyAddress = "0.0.0.0"

func ipv4BindRuntimeEnv(runtime, framework, exposePort string) map[string]string {
	port := strings.TrimSpace(exposePort)
	env := map[string]string{
		"HOST": ipv4AnyAddress,
	}
	if port != "" {
		env["PORT"] = port
	}

	switch strings.ToLower(strings.TrimSpace(runtime)) {
	case "python":
		// ASGI/WSGI commands already pass explicit IPv4 flags; these envs cover
		// simpler autodetected Python entrypoints that read HOST/PORT.
	case "rust":
		if port != "" {
			address := ipv4AnyAddress + ":" + port
			env["ADDRESS"] = address
			env["BIND_ADDR"] = address
			env["BIND_ADDRESS"] = address
			env["LISTEN_ADDR"] = address
			env["SERVER_ADDR"] = address
			env["SOCKET_ADDR"] = address
		}
		switch strings.ToLower(strings.TrimSpace(framework)) {
		case "rocket":
			env["ROCKET_ADDRESS"] = ipv4AnyAddress
			if port != "" {
				env["ROCKET_PORT"] = port
			}
		}
	case "dotnet":
		if port != "" {
			env["ASPNETCORE_URLS"] = "http://" + ipv4AnyAddress + ":" + port
		}
	case "java":
		env["SERVER_ADDRESS"] = ipv4AnyAddress
		if port != "" {
			env["SERVER_PORT"] = port
		}
	case "go":
		if port != "" {
			env["ADDRESS"] = ipv4AnyAddress + ":" + port
			env["BIND_ADDR"] = ipv4AnyAddress + ":" + port
			env["LISTEN_ADDR"] = ipv4AnyAddress + ":" + port
		}
	case "elixir":
		env["BIND_ADDRESS"] = ipv4AnyAddress
	}

	if len(env) == 0 {
		return nil
	}
	return env
}

func mergeRuntimeEnv(base map[string]string, additions map[string]string) map[string]string {
	if len(base) == 0 {
		return additions
	}
	for key, value := range additions {
		if strings.TrimSpace(value) == "" {
			continue
		}
		base[key] = value
	}
	return base
}
