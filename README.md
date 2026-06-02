# apexpack

Build secure, minimal OCI container images from source using [melange](https://github.com/chainguard-dev/melange) and [apko](https://github.com/chainguard-dev/apko) — no Dockerfiles required.

Language support is driven by **YAML profiles** in the `profiles/` directory. Adding a new language means writing one YAML file. Framework-specific build behaviour (Quarkus vs Spring Boot, Maven vs Gradle) is expressed as overrides inside that same file — no Go code required.

---

## Quick Start

```bash
# Build the CLI
go build -o bin/apexpack ./cmd/apexpack

# Detect your project language
./bin/apexpack detect .

# Preview what will be built (no tools run)
./bin/apexpack build . --dry-run

# Build a real OCI image
./bin/apexpack build . --tag ghcr.io/myorg/myapp:v1.0
```

---

## Installation

**Prerequisites:** Go 1.24+, Docker (for macOS builds), melange and apko (for Linux builds)

```bash
# Clone and build
git clone https://github.com/apexpack/apexpack
cd apexpack
go build -o bin/apexpack ./cmd/apexpack

# Or install directly into your Go bin
go install ./cmd/apexpack

# melange and apko are invoked automatically during builds.
# On macOS they run inside Docker (cgr.dev/chainguard/melange and apko images).
# On Linux they run natively — install via: https://github.com/chainguard-dev/melange
#                                           https://github.com/chainguard-dev/apko
```

---

## Commands

### `apexpack detect [source-dir]`

Scans a source directory against all profiles and reports matches, sorted by confidence. Identifies the runtime and the specific framework in use.

```bash
apexpack detect .
apexpack detect /path/to/my-project
apexpack detect . --profiles-dir /custom/profiles
```

Example output:

```
Detected 1 match(es) in .:

→ java          95%  framework: spring-boot   (matched: [pom.xml])

To build: apexpack build .
```

---

### `apexpack build [source-dir]`

Detects the language, loads the matching profile, applies any framework-specific build overrides, generates `melange.yaml` and `apko.yaml`, then runs melange and apko to produce an OCI image tarball.

```bash
# Auto-detect language and build
apexpack build .

# Specify the image tag
apexpack build . --tag ghcr.io/myorg/myapp:v1.0.0

# Skip detection — use a specific runtime profile
apexpack build . --runtime java

# Preview generated configs without running tools
apexpack build . --dry-run

# Custom output directory
apexpack build . --output /tmp/my-build
```

| Flag | Default | Description |
|------|---------|-------------|
| `--tag` / `-t` | `<project>:latest` | OCI image reference |
| `--runtime` | auto-detect | Skip detection, use this profile directly |
| `--version` | `0.0.1` | Version embedded in the APK package |
| `--output` / `-o` | `.apexpack-output/` | Where configs and image tarball are written |
| `--dry-run` | `false` | Print generated configs, do not run tools |
| `--profiles-dir` | `profiles/` | Directory containing language profiles |

---

### `apexpack scan [output-dir]`

Scans the SBOM produced by `apexpack build` for known CVEs using [grype](https://github.com/anchore/grype).

```bash
# Scan the last build (default output dir)
apexpack scan

# Fail if any HIGH or above CVE is found (for CI)
apexpack scan --fail-on high

# Output SARIF for GitHub Code Scanning
apexpack scan --format sarif --output results/
```

| Flag | Default | Description |
|------|---------|-------------|
| `--sbom` | `<output-dir>/sbom-x86_64.spdx.json` | Path to a specific SBOM file |
| `--fail-on` | _(none)_ | Exit 1 at this severity: `critical`, `high`, `medium`, `low` |
| `--format` | `table` | Output format: `table`, `json`, `sarif`, `cyclonedx` |
| `--output` / `-o` | _(none)_ | Write report to this directory |

---

### `apexpack patch [output-dir]`

Compares installed package versions (from the last build SBOM) against the latest Wolfi index and cross-references with grype to identify which outdated packages have CVEs. With `--apply`, updates profile YAML files to pin the patched versions.

```bash
# Show available updates
apexpack patch

# Apply patches — updates profile YAML files
apexpack patch --apply --profiles-dir ./profiles

# Then rebuild to apply
apexpack build .
```

---

### `apexpack profiles`

Lists all loaded language profiles with their detection rules and build dependencies.

```bash
apexpack profiles
apexpack profiles --profiles-dir /custom/profiles
```

---

## How It Works

### The Build Pipeline

```
Source Code
    │
    ▼
detect              Read profiles/*.yaml, match files against source dir
    │               Score by confidence, identify framework from content rules
    ▼
Profile             Language-specific YAML (golang.yaml, java.yaml, etc.)
    │               Applies framework build override if one matches
    ▼
melange.yaml  ──┐
                │  Generated from the profile + project name/version
apko.yaml     ──┘
    │
    ▼
melange             Compiles source → .apk package (Wolfi APK format)
    │               Sandboxed build environment, reproducible output
    ▼
apko                Assembles .apk packages → OCI image
    │               Generates SBOM, minimal Wolfi base, non-root by default
    ▼
OCI Image (.tar)    Ready to push to any registry
```

### Why melange + apko instead of Dockerfile?

| Concern | Dockerfile | melange + apko |
|---------|-----------|----------------|
| Reproducibility | No — `apt-get` results vary | Yes — byte-identical rebuilds |
| SBOM | Manual | Automatic, per-package |
| CVE patching | Rebuild whole image | Swap one APK package |
| Image size | Varies | Minimal — only declared packages |
| Non-root | Manual setup | Default (UID 65532) |
| Build isolation | Shared Docker daemon | Sandboxed per build |

---

## Language Profiles

### Bundled Profiles

| Profile | Detects | Frameworks | Build Tool |
|---------|---------|------------|------------|
| `golang` | `go.mod`, `main.go` | gin, echo, fiber, grpc, connect, root-main | `go build` |
| `java` | `pom.xml`, `build.gradle`, `build.gradle.kts` | spring-boot, quarkus, micronaut (Maven + Gradle variants) | `mvn package` or `./gradlew build` |
| `dotnet` | `*.csproj`, `*.sln` | aspnetcore, masstransit, orleans | `dotnet publish` |
| `node` | `package.json` | nextjs, nestjs, express, fastify, hono, remix | `npm ci && npm run build` |
| `python` | `requirements.txt`, `pyproject.toml`, `Pipfile` | fastapi, django, flask, aiohttp | `pip install` |
| `webserver` | `index.html`, `vite.config.*`, `angular.json` | angular, vite, react, vue, svelte | `npm run build` or static copy |

---

### Profile File Format

Each file in `profiles/` describes one language. The filename becomes the runtime identifier (e.g. `java.yaml` → `runtime: java`).

```yaml
runtime: golang           # unique identifier used by --runtime flag
version: "1"              # profile schema version
description: "Go application (gin, echo, chi, stdlib, gRPC)"

# ── Detection ──────────────────────────────────────────────────────────────
detect:
  # Exact filenames — match if ANY of these exist in the source directory
  files:
    - go.mod
    - main.go

  # Glob patterns — match if ANY pattern matches at least one file
  # Useful when filenames are variable (e.g. MyApp.csproj)
  patterns:
    - "*.csproj"

  # Content rules — read a file and check if it contains a string.
  # The first rule with a non-empty framework that matches sets DetectResult.Framework.
  # More specific frameworks (nextjs) go before broader ones (react) in the list.
  content:
    - file: go.sum
      contains: "gin-gonic"
      boost-confidence: 0.02
      framework: gin
    - file: main.go
      contains: "func main"
      framework: root-main

  confidence: 0.85   # base confidence score (0.0–1.0) when files/patterns match

# ── Build (feeds into melange.yaml) ────────────────────────────────────────
build:
  dependencies:        # APK packages in the BUILD environment (not in the final image)
    - go
    - build-base
    - git
  command: |           # default shell command — used when no framework override matches
    mkdir -p ${{targets.destdir}}/usr/bin
    go build -o ${{targets.destdir}}/usr/bin/app .
  env:
    CGO_ENABLED: "0"
    GOFLAGS: "-trimpath"

  # Framework overrides — only define fields that differ from the defaults above.
  # Any unset field (dependencies, command, env) falls back to the build defaults.
  frameworks:
    quarkus:
      command: |       # replaces build.command for Quarkus Maven projects
        mvn package -DskipTests -B -q -Dquarkus.package.jar.type=uber-jar
        mkdir -p /home/build/output/app
        cp target/*-runner.jar /home/build/output/app/app.jar
    quarkus-gradle:
      dependencies:    # replaces build.dependencies for this framework
        - busybox
        - openjdk-21
        - build-base
        - git
      command: |
        chmod +x ./gradlew
        ./gradlew build -Dquarkus.package.jar.type=uber-jar -x test --no-daemon -q
        mkdir -p /home/build/output/app
        cp build/quarkus-app/*-runner.jar /home/build/output/app/app.jar

# ── Image (feeds into apko.yaml) ────────────────────────────────────────────
image:
  packages:            # APK packages in the FINAL image — keep minimal
    - ca-certificates-bundle
  entrypoint: /usr/bin/app
  cmd: []
  run-as: 65532        # UID — never 0/root
  ports:
    - "8080"
  env:
    APP_ENV: "production"
```

#### How framework detection and overrides work

Detection content rules set a `framework` name (e.g. `spring-boot`, `quarkus-gradle`). The `build.frameworks` map keys must match these names exactly. When a framework is detected, its entry in the `frameworks` map is applied on top of the defaults — only the fields you define are overridden.

The framework name encodes both the framework AND the build tool when needed. For example, `spring-boot` means Maven + Spring Boot, while `spring-boot-gradle` means Gradle + Spring Boot. Detection rules produce the right name based on which files are present:

```yaml
content:
  - file: pom.xml
    contains: "spring-boot"
    framework: spring-boot          # Maven — uses default mvn command
  - file: build.gradle
    contains: "spring-boot"
    framework: spring-boot-gradle   # Gradle — triggers the gradle override
```

Only add a framework entry when it actually differs from the default. If all detected frameworks use the same build command, no `frameworks` section is needed (see `golang.yaml`).

---

### Per-project `apexpack.yaml` overrides

Place an `apexpack.yaml` in the project root to override or extend the detected profile. Only set what you need to change — everything else is inherited from the profile.

```yaml
# apexpack.yaml
runtime: golang          # optional — overrides auto-detection

build:
  command: |             # replaces build.command from the profile
    mkdir -p ${{targets.destdir}}/usr/bin
    go build -o ${{targets.destdir}}/usr/bin/app ./cmd/myapp
  env:                   # merged on top of profile build.env
    CGO_ENABLED: "1"
  dependencies:          # appended to profile build.dependencies
    - sqlite-libs

image:
  packages:              # appended to profile image.packages
    - sqlite-libs
  env:                   # merged on top of profile image.env
    DATABASE_PATH: "/data/app.db"
  entrypoint: /usr/bin/myapp   # replaces profile entrypoint
```

#### Go projects with `cmd/` layout

Go projects where `main.go` lives in `cmd/<name>/` (not at the root) need this override, since `go build .` has nothing to compile at the root:

```yaml
# apexpack.yaml
runtime: golang
build:
  command: |
    mkdir -p ${{targets.destdir}}/usr/bin
    go build -o ${{targets.destdir}}/usr/bin/app ./cmd/myapp
```

---

### Adding a New Language Profile

1. Create `profiles/<runtime>.yaml`
2. Define `runtime`, `detect`, `build`, `image` (all required)
3. Add `frameworks` entries only for cases that need a different `command`, `dependencies`, or `env`
4. Run `apexpack profiles` to verify it loads
5. Run `apexpack detect /path/to/sample-project` to test detection
6. Run `apexpack build /path/to/sample-project --dry-run` to verify generated configs

Example — Rust:

```yaml
runtime: rust
version: "1"
description: "Rust application (axum, actix-web)"

detect:
  files:
    - Cargo.toml
  content:
    - file: Cargo.toml
      contains: "axum"
      boost-confidence: 0.04
      framework: axum
    - file: Cargo.toml
      contains: "actix-web"
      boost-confidence: 0.04
      framework: actix-web
  confidence: 0.85

build:
  dependencies:
    - rust
    - build-base
    - git
  command: |
    cargo build --release
    mkdir -p /home/build/output/app
    cp target/release/$(basename $PWD) /home/build/output/app/app
  env:
    CARGO_NET_OFFLINE: "false"

image:
  packages:
    - ca-certificates-bundle
  entrypoint: /usr/bin/app
  run-as: 65532
  ports:
    - "8080"
```

---

## How Detection Confidence Works

```
Base confidence (from detect.confidence)      e.g. 0.85
+ boost from each matching content rule       e.g. +0.05 (spring-boot found in pom.xml)
+ boost from each matching content rule       e.g. +0.02 (additional signal)
─────────────────────────────────────────────────────────
Final confidence                              e.g. 0.92  (capped at 1.0)
```

When multiple profiles match (e.g. a Next.js project matches both `node` and `webserver` because it has an `index.html`), results are sorted highest-confidence first. The `build` command uses the top result automatically. Use `--runtime` to override.

---

## Project Structure

```
apexpack/
│
├── cmd/
│   └── apexpack/
│       └── main.go          CLI entry point — detect, build, scan, patch, profiles
│
├── internal/
│   ├── types/
│   │   └── types.go         ALL data structures — Profile, DetectResult, BuildPlan, etc.
│   │                        Read this first to understand the shape of everything.
│   │
│   ├── profile/
│   │   └── profile.go       Loads profiles/*.yaml, validates, merges project overrides.
│   │                        LoadAll() → []*Profile
│   │                        LoadProjectConfig() → *ProjectConfig
│   │
│   ├── detect/
│   │   └── detect.go        Matches profiles against a source directory.
│   │                        Run() → []DetectResult sorted by confidence
│   │                        Best() → *DetectResult (highest confidence only)
│   │
│   ├── build/
│   │   └── build.go         Generates melange.yaml + apko.yaml, runs tools.
│   │                        Plan() → *BuildPlan (generate only, no tools run)
│   │                        Run()  → executes melange then apko
│   │
│   └── patch/
│       └── patch.go         Checks Wolfi index for updates, applies pins to profiles.
│
├── profiles/                Language profile YAML files
│   ├── golang.yaml
│   ├── java.yaml
│   ├── dotnet.yaml
│   ├── node.yaml
│   ├── python.yaml
│   └── webserver.yaml
│
└── tekton/                  Tekton CI/CD resources
    ├── tasks/               apexpack-detect, apexpack-build, apexpack-scan Tasks
    ├── pipelines/           build-and-push Pipeline
    └── config/              ConfigMap, ArgoCD and Flux integration
```

### Data Flow

```
profiles/*.yaml
      │
      │  profile.LoadAll()
      ▼
[]*types.Profile
      │
      │  detect.Run(profiles, srcDir)
      ▼
[]types.DetectResult   { Profile, Confidence, Framework, MatchedFiles }
      │
      │  build.Plan(profile, opts)   ← applies framework override if matched
      ▼
*types.BuildPlan       { MelangeConfig, ApkoConfig, Framework }
      │
      │  build.Run(plan, opts)
      ▼
melange → .apk package
apko    → OCI image tarball + SBOM
```

### Key Design Decisions

**1. `types.go` is the centre of everything**
Every package imports `internal/types`. Nothing else imports from `cmd/`. This keeps dependency direction clean and prevents circular imports.

**2. Framework overrides are sparse**
A `frameworks` entry only needs to define what differs from the profile defaults. Unset fields (`dependencies`, `command`, `env`) are inherited. Only add a framework entry when the command or deps genuinely change.

**3. Detection encodes the build tool**
For languages with multiple build tools (Maven vs Gradle for Java), the content rules produce distinct framework names (`spring-boot` vs `spring-boot-gradle`). This keeps build commands unconditional — no `if [ -f pom.xml ]` in YAML commands.

**4. Plan and Run are separated**
`build.Plan()` generates config content. `build.Run()` writes files and runs tools. The `--dry-run` flag uses Plan without Run, so you can inspect exactly what will be built before committing.

**5. Profiles directory is runtime — not embedded**
Profiles live on disk in `profiles/`, not compiled into the binary. You can add, edit, or override profiles without recompiling. Teams can maintain a shared profiles repo and point `--profiles-dir` at it.

---

## Tekton Pipeline Integration

apexpack ships Tekton Tasks and a Pipeline that clone the source, detect the language, build the image, scan for CVEs, and push — all driven by the YAML profiles.

```yaml
# Example PipelineRun
apiVersion: tekton.dev/v1
kind: PipelineRun
metadata:
  name: build-my-app
spec:
  pipelineRef:
    name: apexpack-build-and-push
  params:
    - name: GIT_URL
      value: https://github.com/myorg/myapp
    - name: IMAGE
      value: ghcr.io/myorg/myapp:v1.0.0
    - name: FAIL_ON
      value: high
  workspaces:
    - name: source
      volumeClaimTemplate:
        spec:
          accessModes: [ReadWriteOnce]
          resources:
            requests:
              storage: 1Gi
    - name: profiles
      volumeClaimTemplate:
        spec:
          accessModes: [ReadWriteOnce]
          resources:
            requests:
              storage: 100Mi
    - name: output
      volumeClaimTemplate:
        spec:
          accessModes: [ReadWriteOnce]
          resources:
            requests:
              storage: 2Gi
```

The pipeline steps:

| Step | Task | Description |
|------|------|-------------|
| 1a | `git-clone` | Clone the profiles repo |
| 1b | `git-clone` | Clone the source repo |
| 2 | `apexpack-detect` | Detect language and framework |
| 3 | `apexpack-build` | Build OCI image (privileged — melange uses bubblewrap) |
| 4 | `apexpack-scan` | Scan SBOM for CVEs with grype |
| 5 | `crane-copy` | Push image tarball to registry |

---

## Contributing

### Adding a language profile

1. Create `profiles/<runtime>.yaml`
2. Define `runtime`, `detect`, `build`, `image` (all required)
3. Add `frameworks` entries for any cases that need a different `command`, `dependencies`, or `env`
4. Run `apexpack profiles` to verify it loads
5. Run `apexpack detect /path/to/sample-project` to test detection
6. Run `apexpack build /path/to/sample-project --dry-run` to verify generated configs

### Modifying the Go code

All data structures are in `internal/types/types.go`. Change the struct there first, then update the code that reads or writes those fields. Go changes are only needed when:

- Adding a new detection method (currently: exact files, glob patterns, content string match)
- Adding a new build output format (currently: melange + apko)
- Adding a new CLI command

The four internal packages have a strict one-way dependency: `types` ← `profile`, `detect`, `build`, `patch`. Nothing imports from `cmd/`.

---

## Acknowledgements

- **[melange](https://github.com/chainguard-dev/melange)** — APK package builder from source, by Chainguard
- **[apko](https://github.com/chainguard-dev/apko)** — OCI image assembler from APK packages, by Chainguard
- **[Wolfi](https://github.com/wolfi-dev/os)** — supply chain-hardened Linux undistro, by Chainguard
- **[cobra](https://github.com/spf13/cobra)** — CLI framework for Go
- **[grype](https://github.com/anchore/grype)** — vulnerability scanner for container images, by Anchore
