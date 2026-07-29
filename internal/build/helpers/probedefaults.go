package helpers

// FrameworkProbeDefaults returns a default HTTP health probe path and port for
// known frameworks. Returns ("", 0) for frameworks with no standard health endpoint.
func FrameworkProbeDefaults(framework string) (path string, port int) {
	switch framework {
	case "spring-boot", "spring-boot-gradle":
		return "/actuator/health", 8080
	case "quarkus", "quarkus-gradle":
		return "/q/health", 8080
	case "micronaut", "micronaut-gradle":
		return "/health", 8080
	case "aspnetcore":
		return "/health", 8080
	case "masstransit", "orleans":
		return "", 0
	case "gin", "echo", "fiber", "connect":
		return "/health", 8080
	case "grpc", "root-main":
		return "", 0
	case "express", "fastify", "nestjs", "hono":
		return "/health", 3000
	case "nextjs", "remix":
		return "", 0
	case "fastapi", "aiohttp":
		return "/health", 8000
	case "flask":
		return "/health", 5000
	case "django":
		return "", 0
	default:
		return "", 0
	}
}
