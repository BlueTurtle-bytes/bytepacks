# internal/build — Package Structure

`build.go` is the orchestrator. Everything else lives in a focused subpackage.

---

## build.go

Entry point for the build pipeline.

| Export | Purpose |
|--------|---------|
| `Plan(profile, opts)` | Generates `MelangeConfig` + `ApkoConfig` by calling `config/` then dispatching to `hooks/` |
| `Run(plan, opts)` | Executes the build: melange → apko → healthcheck → probes |
| `Options` | Type alias for `types.BuildOptions` (backward compat for cmd layer) |
| `SanitizeImageName()` | Re-exported from `helpers/` |
| `applyDefaults()` | Fills in version, project name, output dir if not set |

---

## helpers/

Pure utility functions. No external process calls, no I/O side effects.

| File | What it does |
|------|--------------|
| `version.go` | `LangVersionToken`, `ResolveVersion`, `ValidateRuntimeVersion`, `Vsub`, `VsubSlice`, `VsubMap` — token substitution (`{JAVA_VERSION}` → `17`) and version validation per runtime |
| `util.go` | `SanitizeImageName`, `ApplyProjectTemplates`, `ReadProcfileCmd`, `CacheVolumeName`, `ReadNodeEntrypoint` — string cleanup and file-reading helpers |
| `override.go` | `ResolveOverride` — looks up a framework+package-manager specific build override from a profile (e.g. `nextjs-pnpm`) |
| `probedefaults.go` | `FrameworkProbeDefaults` — returns default probe path and port per framework (e.g. fastapi → `/health`, `8000`) |

---

## templates/

Embedded config file templates loaded at runtime. Files are baked into the binary via `//go:embed`.

| File | What it does |
|------|--------------|
| `template.go` | `LoadMavenTemplate`, `LoadNuGetTemplate`, `LoadGradleTemplate` — reads embedded XML/Gradle files and returns them as strings |
| `maven/maven_settings.xml` | Maven `settings.xml` injected into melange builds to configure proxy/mirror |
| `nuget/nuget_config.xml` | NuGet `NuGet.Config` injected for .NET builds |
| `gradle/gradle_init.gradle` | Gradle init script injected to configure repo URLs and SSL certs |

---

## hooks/

Language-specific patches applied on top of the base melange/apko config. Each hook implements the `LanguageHook` interface.

| File | What it does |
|------|--------------|
| `hook.go` | `LanguageHook` interface (`PatchMelange`, `PatchApko`), hook registry, `Get(runtime)` lookup |
| `hook_java.go` | Java hook — resolves JDK version, injects `JAVA_HOME`, writes maven settings or gradle init script, handles Java 8 path quirk (`java-1.8`) |
| `hook_dotnet.go` | .NET hook — substitutes `{DOTNET_VERSION}` tokens, injects NuGet config |
| `hook_node.go` | Node hook — resolves node version token, reads `package.json` entrypoint, handles yarn/pnpm/npm variants |
| `hook_python.go` | Python hook — substitutes `{PYTHON_VERSION}`, injects system deps detected from `requirements.txt` |
| `hook_go.go` | Go hook — substitutes `{GO_VERSION}`, sets `GOPATH`/cache env |
| `hook_webserver.go` | Webserver profile hook — nginx config placement, runtime dir setup |

---

## config/

Generates `MelangeConfig` and `ApkoConfig` structs from a profile and options. No knowledge of hooks or external processes.

| File | What it does |
|------|--------------|
| `config.go` | `BuildMelangeConfig` — builds the melange pipeline YAML struct; `BuildApkoConfig` — builds the apko image config struct; `BuildHealthCheck` — converts profile health check config to apko health check; `EnsurePackage` — deduplicates package lists; `MarshalYAML` — serializes a config struct to YAML bytes |

---

## runner/

Executes external tools (melange, apko, docker). All I/O side effects live here.

| File | What it does |
|------|--------------|
| `runner.go` | `RunMelange`, `RunMelangeTest`, `RunApko` — orchestrates tool invocations; `ensureSigningKey`, `copySigningKey` — manages melange signing key lifecycle |
| `exec.go` | Low-level process helpers: `runTool`, `runToolInDir`, `runToolEnv`; `melangeArch` — normalises arch string; `archToDockerPlatform` — maps `aarch64` → `linux/arm64` |
| `ca.go` | `readCACerts`, `mergeCABundles` — reads and merges CA cert bundles for TLS injection into builds |
| `healthcheck.go` | `InjectHealthCheckIntoTar` — patches the apko output tarball to embed a health check; `RunHealthCheckTest` — spins up a container to verify the health check passes |

---

## probes/

Generates Kubernetes readiness/liveness probe YAML alongside the built image.

| File | What it does |
|------|--------------|
| `probes.go` | `EmitProbesYAML` — writes a `probes.yaml` file next to the image with HTTP or TCP probe definitions ready to paste into a Deployment spec |

---

## Dependency graph (no cycles)

```
build.go
  ├── config/      (no imports from this module)
  ├── hooks/
  │     └── helpers/, templates/
  ├── runner/      (no imports from this module)
  └── probes/
        └── helpers/

helpers/    — leaf, imports types/ only
templates/  — leaf, no internal imports
config/     — imports types/, helpers/
hooks/      — imports types/, helpers/, templates/
runner/     — imports types/
probes/     — imports types/, helpers/
```
