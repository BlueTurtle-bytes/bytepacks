# apexpack

Build secure, minimal OCI container images from source using [melange](https://github.com/chainguard-dev/melange) and [apko](https://github.com/chainguard-dev/apko) — no Dockerfiles required.

Language support is driven by **YAML profiles** in the `profiles/` directory. Adding a new language means writing one YAML file. Framework-specific and package-manager-specific build behaviour is expressed as overrides inside that same file — no Go code required.

---

## Quick Start

```bash
# Build the CLI
go build -o bin/apexpack ./cmd/apexpack

# Detect your project language and package manager
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

Scans a source directory against all profiles and reports matches sorted by confidence. Identifies the runtime, the specific framework, and the package manager in use.

```bash
apexpack detect .
apexpack detect /path/to/my-project
apexpack detect . --profiles-dir /custom/profiles
```

Example output:

```
Detected 1 match(es) in .:

→ node           90%  framework: nextjs   (matched: [package.json])

To build: apexpack build .
```

For a pnpm project the same command also picks up the package manager:

```
→ node           90%  framework: nextjs   (matched: [package.json, pnpm-lock.yaml])
```

---

### `apexpack build [source-dir]`

Detects the language, loads the matching profile, resolves any framework or package-manager build overrides, generates `melange.yaml` and `apko.yaml`, then runs melange and apko to produce an OCI image tarball. Build caches are mounted as named Docker volumes on macOS so package managers reuse their download caches across builds.

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

# Corporate proxy — trust an extra CA certificate
apexpack build . --tls-extra-ca ~/corp-ca.pem
```

| Flag | Default | Description |
|------|---------|-------------|
| `--tag` / `-t` | `<project>:latest` | OCI image reference |
| `--runtime` | auto-detect | Skip detection, use this profile directly |
| `--version` | `0.0.1` | Version embedded in the APK package |
| `--output` / `-o` | `.apexpack-output/` | Where configs and image tarball are written |
| `--dry-run` | `false` | Print generated configs, do not run tools |
| `--profiles-dir` | `profiles/` | Directory containing language profiles |
| `--tls-extra-ca` | _(none)_ | Path to an extra CA cert (PEM) to trust — for corporate proxy environments (env: `APEXPACK_EXTRA_CA`) |

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
    │               Score by confidence
    │               Identify framework (content rules) and package manager (file existence)
    ▼
Profile             Language-specific YAML (golang.yaml, java.yaml, etc.)
    │               Resolves build override: {framework}-{pm} → {pm} → {framework} → default
    ▼
melange.yaml  ──┐
                │  Generated from the resolved profile + project name/version
apko.yaml     ──┘   Image entrypoint from Procfile if profile has none
    │
    ▼
melange             Compiles source → .apk package (Wolfi APK format)
    │               Named Docker volumes provide package manager caches (macOS)
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

| Profile | Detects | Frameworks | Package Managers |
|---------|---------|------------|-----------------|
| `golang` | `go.mod`, `main.go` | gin, echo, fiber, grpc, connect, root-main | _(go modules only)_ |
| `java` | `pom.xml`, `build.gradle`, `build.gradle.kts` | spring-boot, quarkus, micronaut (+ gradle variants) | _(Maven or Gradle via framework name)_ |
| `dotnet` | `*.csproj`, `*.sln` | aspnetcore, masstransit, orleans | _(dotnet CLI only)_ |
| `node` | `package.json` | nextjs, nestjs, express, fastify, hono, remix | npm _(default)_, pnpm, bun, yarn, yarn-berry |
| `python` | `requirements.txt`, `pyproject.toml`, `Pipfile` | fastapi, django, flask, aiohttp | pip _(default)_, uv, poetry, pipenv |
| `webserver` | `index.html`, `vite.config.*`, `angular.json` | angular, vite, react, vue, svelte | _(npm only)_ |

---

### Profile File Format

Each file in `profiles/` describes one language. The filename becomes the runtime identifier (e.g. `java.yaml` → `runtime: java`).

```yaml
runtime: node             # unique identifier used by --runtime flag
version: "1"              # profile schema version
description: "Node.js application (Express, Fastify, NestJS, Next.js)"

# ── Detection ──────────────────────────────────────────────────────────────
detect:
  # Exact filenames — match if ANY of these exist in the source directory
  files:
    - package.json

  # Glob patterns — match if ANY pattern matches at least one file
  patterns:
    - "*.csproj"

  # Package manager rules — checked by file existence, first match wins.
  # Sets DetectResult.PackageManager independently of the framework.
  package-managers:
    - file: bun.lockb
      manager: bun
    - file: pnpm-lock.yaml
      manager: pnpm
    - file: .yarnrc.yml
      manager: yarn-berry
    - file: yarn.lock
      manager: yarn

  # Content rules — read a file and check if it contains a string.
  # The first rule with a non-empty framework that matches sets DetectResult.Framework.
  content:
    - file: package.json
      contains: "\"next\""
      boost-confidence: 0.05
      framework: nextjs
    - file: package.json
      contains: "\"express\""
      boost-confidence: 0.03
      framework: express

  confidence: 0.85   # base confidence score (0.0–1.0) when files/patterns match

# ── Build (feeds into melange.yaml) ────────────────────────────────────────
build:
  dependencies:        # APK packages in the BUILD environment (not in the final image)
    - nodejs
    - npm
    - git
  command: |           # default — used when no framework/package-manager override matches
    npm ci --prefer-offline
    npm run build --if-present
    mkdir -p /home/build/output/app
    cp -r . /home/build/output/app/
    rm -rf /home/build/output/app/node_modules
    cd /home/build/output/app && npm ci --omit=dev --prefer-offline
  env:
    NODE_ENV: "production"
    NPM_CONFIG_CACHE: "/home/build/.npm"
  caches:              # paths persisted as named Docker volumes between builds (macOS)
    - /home/build/.npm

  # Framework/package-manager overrides.
  # Only define fields that differ from the defaults above.
  # Lookup order: {framework}-{packageManager} → {packageManager} → {framework} → default
  frameworks:
    pnpm:              # any project using pnpm, regardless of framework
      dependencies:
        - nodejs
        - npm
        - git
      command: |
        npm install -g pnpm
        pnpm install --frozen-lockfile
        pnpm run build --if-present
        mkdir -p /home/build/output/app
        cp -r . /home/build/output/app/
        cd /home/build/output/app && pnpm install --frozen-lockfile --prod
      env:
        NODE_ENV: "production"
        PNPM_HOME: "/home/build/.local/share/pnpm"
      caches:
        - /home/build/.local/share/pnpm/store
    bun:
      dependencies:
        - bun
        - git
      command: |
        bun install --frozen-lockfile
        bun run build --if-present
        mkdir -p /home/build/output/app
        cp -r . /home/build/output/app/
        cd /home/build/output/app && bun install --frozen-lockfile --production
      caches:
        - /home/build/.bun/install/cache

# ── Image (feeds into apko.yaml) ────────────────────────────────────────────
image:
  packages:
    - nodejs
    - ca-certificates-bundle
  entrypoint: node
  cmd:
    - "/app/server.js"
  run-as: 65532
  ports:
    - "3000"
  env:
    NODE_ENV: "production"
```

---

### Detection: framework vs package manager

These are two independent dimensions detected separately and combined during build override resolution.

**Framework** is set by the first matching `content` rule with a non-empty `framework` field. It identifies _what_ the app is built with (Spring Boot, Next.js, Quarkus, etc.).

**Package manager** is set by the first matching `package-managers` rule. It identifies _how_ dependencies are installed (pnpm, bun, poetry, uv, etc.), detected purely by file existence — no content reading required.

```
Detected framework:        nextjs
Detected package manager:  pnpm

Override lookup order:
  1. "nextjs-pnpm"   → not in frameworks map
  2. "pnpm"          → found! use pnpm command and caches   ✓
  3. "nextjs"        → (skipped)
  4. default         → (skipped)
```

A `nextjs-pnpm` entry would only be needed if Next.js + pnpm requires something different from pnpm alone. In practice, the package manager entry handles all frameworks uniformly.

For Java, build tool (Maven vs Gradle) is encoded directly in the framework name (`spring-boot` vs `spring-boot-gradle`) because Gradle and Maven are detected by file presence (`pom.xml` vs `build.gradle`) and affect the framework command fundamentally:

```yaml
content:
  - file: pom.xml
    contains: "spring-boot"
    framework: spring-boot          # Maven — uses default mvn command
  - file: build.gradle
    contains: "spring-boot"
    framework: spring-boot-gradle   # Gradle — triggers gradle override
```

---

### Build caching

Each profile and framework entry can declare `caches` — a list of absolute paths inside the build container to persist between runs. On macOS, each path is mounted as a named Docker volume (`apexpack-cache-*`). This means npm, pnpm, Maven, Gradle, pip, uv, and Go module caches survive between builds without re-downloading packages.

```yaml
build:
  caches:
    - /home/build/.m2/repository   # Maven local repo

  frameworks:
    spring-boot-gradle:
      caches:
        - /home/build/.gradle      # replaces build.caches for this framework
```

Framework-level `caches` replace (not append to) the top-level `build.caches`. If a framework uses a different cache location, declare it in the framework entry.

---

### Procfile support

If a project has a `Procfile` with a `web:` process and the detected profile has no explicit `image.entrypoint`, apexpack parses the Procfile and uses the `web:` command as the container entrypoint:

```
# Procfile
web: node dist/server.js
worker: node dist/worker.js
```

Results in:
```yaml
entrypoint:
  command: node
cmd: dist/server.js
```

The profile's `image.entrypoint` always takes precedence. Procfile is only used as a fallback when the profile leaves entrypoint empty.

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
3. Add `package-managers` rules if the language supports multiple build tools
4. Add `frameworks` entries only for cases that need different `command`, `dependencies`, `env`, or `caches`
5. Run `apexpack profiles` to verify it loads
6. Run `apexpack detect /path/to/sample-project` to test detection
7. Run `apexpack build /path/to/sample-project --dry-run` to verify generated configs

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
  caches:
    - /home/build/.cargo/registry
    - /home/build/.cargo/git

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
Base confidence (from detect.confidence)         e.g. 0.85
+ boost from each matching content rule          e.g. +0.05 (spring-boot found in pom.xml)
+ boost from each matching content rule          e.g. +0.02 (additional signal)
──────────────────────────────────────────────────────────
Final confidence                                 e.g. 0.92  (capped at 1.0)
```

Package manager rules do not affect confidence — they only set `DetectResult.PackageManager`. When multiple profiles match, results are sorted highest-confidence first. The `build` command uses the top result automatically. Use `--runtime` to override.

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
[]types.DetectResult   { Profile, Confidence, Framework, PackageManager, MatchedFiles }
      │
      │  build.Plan(profile, opts)
      │    resolves: {framework}-{pm} → {pm} → {framework} → default
      │    reads Procfile for fallback entrypoint
      ▼
*types.BuildPlan       { MelangeConfig, ApkoConfig, Framework, PackageManager, ProcfileCmd }
      │
      │  build.Run(plan, opts)
      │    mounts named Docker volumes for declared caches (macOS)
      ▼
melange → .apk package
apko    → OCI image tarball + SBOM
```

### Key Design Decisions

**1. `types.go` is the centre of everything**
Every package imports `internal/types`. Nothing else imports from `cmd/`. This keeps dependency direction clean and prevents circular imports.

**2. Framework and package manager are detected independently**
`framework` comes from content rules (what the app uses). `PackageManager` comes from file-existence rules (how deps are installed). They combine during build override resolution using a three-level fallback, so a single `pnpm` entry covers all frameworks using pnpm without requiring `nextjs-pnpm`, `nestjs-pnpm`, etc.

**3. Framework overrides are sparse**
A `frameworks` entry only needs to define what differs from the profile defaults. Unset fields (`dependencies`, `command`, `env`, `caches`) are inherited. Only add a framework entry when the command, deps, or caches genuinely change.

**4. Build tool encoded in framework name for Java**
For Java, Maven vs Gradle is a fundamental build tool difference that changes the command entirely. Encoding it in the framework name (`spring-boot` vs `spring-boot-gradle`) keeps commands unconditional — no `if [ -f pom.xml ]` in YAML.

**5. Caches are named Docker volumes**
On macOS, each declared cache path becomes a persistent named Docker volume. The volume name is derived from the path, so the same cache is reused across all builds of that project type.

**6. Plan and Run are separated**
`build.Plan()` generates config content. `build.Run()` writes files and runs tools. The `--dry-run` flag uses Plan without Run, so you can inspect exactly what will be built — including which package manager override fired — before committing.

**7. Profiles directory is runtime — not embedded**
Profiles live on disk in `profiles/`, not compiled into the binary. Add, edit, or override profiles without recompiling. Teams can maintain a shared profiles repo and point `--profiles-dir` at it.

---

## Corporate Proxy Environments

In networks where a TLS-intercepting proxy (Zscaler, Blue Coat, etc.) replaces certificates, the melange build container will fail to reach `packages.wolfi.dev` with an x509 certificate error:

```
failed to verify the x509 cert: signed by unknown authority
```

The fix is to provide the corporate CA certificate so the melange container trusts it alongside the standard system CAs.

**Step 1 — get the corporate CA certificate (PEM format)**

```bash
# macOS — export from system Keychain
security find-certificate -a -p /Library/Keychains/System.keychain > ~/corp-ca.pem

# Linux — usually already on disk
cp /usr/local/share/ca-certificates/corporate.crt ~/corp-ca.pem
```

Or ask your IT / security team for the root CA certificate in PEM format.

**Step 2 — pass it to apexpack**

```bash
# Via flag
apexpack build . --tls-extra-ca ~/corp-ca.pem

# Via environment variable — set once in your shell profile
export APEXPACK_EXTRA_CA=~/corp-ca.pem
apexpack build .
```

The flag takes precedence over the environment variable. Both the system CAs and the corporate CA are trusted — the corporate cert is added alongside, not instead of, the container's existing trust store.

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
| 2 | `apexpack-detect` | Detect language, framework, and package manager |
| 3 | `apexpack-build` | Build OCI image (privileged — melange uses bubblewrap) |
| 4 | `apexpack-scan` | Scan SBOM for CVEs with grype |
| 5 | `crane-copy` | Push image tarball to registry |

---

## Contributing

### Adding a language profile

1. Create `profiles/<runtime>.yaml`
2. Define `runtime`, `detect`, `build`, `image` (all required)
3. Add `package-managers` rules if the language has multiple build tools (pnpm, uv, etc.)
4. Add `frameworks` entries for any cases that need a different `command`, `dependencies`, `env`, or `caches`
5. Run `apexpack profiles` to verify it loads
6. Run `apexpack detect /path/to/sample-project` to test detection
7. Run `apexpack build /path/to/sample-project --dry-run` to verify generated configs

### Modifying the Go code

All data structures are in `internal/types/types.go`. Change the struct there first, then update the code that reads or writes those fields. Go changes are only needed when:

- Adding a new detection method (currently: exact files, glob patterns, content string match, package manager file existence)
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
