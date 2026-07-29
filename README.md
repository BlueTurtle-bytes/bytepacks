# apexpack

Build secure, minimal OCI container images from source using [melange](https://github.com/chainguard-dev/melange) and [apko](https://github.com/chainguard-dev/apko) — no Dockerfiles required.

Language support is driven by **YAML profiles** in the `profiles/` directory. Adding a new language means writing one YAML file. Framework-specific and package-manager-specific build behaviour is expressed as overrides inside that same file — no Go code required.

---

## Build Status

> Verified on every push to `main` against reference sample repositories.
> Replace `YOUR_GIST_ID` below with your Gist ID once secrets are configured.

**Java** &nbsp;
![Java 17](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/BlueTurtle-bytes/YOUR_GIST_ID/raw/java_17.json&cacheSeconds=300)
![Java 21](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/BlueTurtle-bytes/YOUR_GIST_ID/raw/java_21.json&cacheSeconds=300)

**Node** &nbsp;
![Node 18](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/BlueTurtle-bytes/YOUR_GIST_ID/raw/node_18.json&cacheSeconds=300)
![Node 20](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/BlueTurtle-bytes/YOUR_GIST_ID/raw/node_20.json&cacheSeconds=300)
![Node 22](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/BlueTurtle-bytes/YOUR_GIST_ID/raw/node_22.json&cacheSeconds=300)

**Python** &nbsp;
![Python 3.11](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/BlueTurtle-bytes/YOUR_GIST_ID/raw/python_311.json&cacheSeconds=300)
![Python 3.12](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/BlueTurtle-bytes/YOUR_GIST_ID/raw/python_312.json&cacheSeconds=300)

**.NET** &nbsp;
![.NET 8](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/BlueTurtle-bytes/YOUR_GIST_ID/raw/dotnet_8.json&cacheSeconds=300)
![.NET 9](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/BlueTurtle-bytes/YOUR_GIST_ID/raw/dotnet_9.json&cacheSeconds=300)

**Go** &nbsp;
![Go 1.22](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/BlueTurtle-bytes/YOUR_GIST_ID/raw/go_122.json&cacheSeconds=300)
![Go 1.23](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/BlueTurtle-bytes/YOUR_GIST_ID/raw/go_123.json&cacheSeconds=300)

---

## Quick Start

```bash
# Install the binary (see Installation section below)
# Then, in any project directory:

apexpacks doctor              # check all required tools are installed
apexpacks detect .            # detect language and framework
apexpacks build . --dry-run  # preview what will be built
apexpacks build .             # build an OCI image
apexpacks scan                # scan for CVEs
```

---

## Installation

### Download a pre-built binary (recommended)

Pre-built binaries for macOS and Linux are published with every GitHub release. No Go toolchain required.

#### macOS (Apple Silicon or Intel)

```bash
# Apple Silicon (M1/M2/M3)
curl -L https://github.com/apexpack/apexpack/releases/latest/download/apexpacks_darwin_arm64.tar.gz | tar xz
sudo mv apexpacks /usr/local/bin/

# Intel Mac
curl -L https://github.com/apexpack/apexpack/releases/latest/download/apexpacks_darwin_amd64.tar.gz | tar xz
sudo mv apexpacks /usr/local/bin/
```

Verify the install:

```bash
apexpacks version
```

> If macOS blocks the binary with "cannot be opened because the developer cannot be verified", run:
> `xattr -d com.apple.quarantine /usr/local/bin/apexpacks`

#### Ubuntu / Debian (x86-64 or ARM64)

```bash
# x86-64
curl -L https://github.com/apexpack/apexpack/releases/latest/download/apexpacks_linux_amd64.tar.gz | tar xz
sudo mv apexpacks /usr/local/bin/

# ARM64
curl -L https://github.com/apexpack/apexpack/releases/latest/download/apexpacks_linux_arm64.tar.gz | tar xz
sudo mv apexpacks /usr/local/bin/
```

Verify:

```bash
apexpacks version
```

#### Install a specific version

Replace `latest/download` with `download/v0.x.y` to pin to a specific release:

```bash
curl -L https://github.com/apexpack/apexpack/releases/download/v0.2.0/apexpacks_darwin_arm64.tar.gz | tar xz
```

#### Verify the checksum

Each release includes a `checksums.txt` file:

```bash
# Download the binary and the checksum file
curl -LO https://github.com/apexpack/apexpack/releases/latest/download/apexpacks_darwin_arm64.tar.gz
curl -LO https://github.com/apexpack/apexpack/releases/latest/download/checksums.txt

# Verify
shasum -a 256 --check checksums.txt --ignore-missing
```

---

### Build from source

**Prerequisites:** Go 1.25+, Docker (for macOS builds), melange and apko (for Linux builds)

```bash
git clone https://github.com/apexpack/apexpack
cd apexpack
go build -o bin/apexpacks ./cmd/apexpacks

# Or install directly into your Go bin
go install ./cmd/apexpacks
```

On macOS, melange and apko run inside Docker (no native install needed). On Linux, install them natively:
- melange: https://github.com/chainguard-dev/melange
- apko: https://github.com/chainguard-dev/apko

**Corporate / proxy environments:** if your network uses TLS inspection (Zscaler, etc.), see the [Corporate Proxy Environments](#corporate-proxy-environments) section before running `go build`.

---

## Commands

### `apexpacks doctor`

Checks that all tools required by apexpacks are installed and reachable in `PATH`. On macOS, melange and apko are marked optional because they run inside Docker automatically — Docker itself is required. On Linux, all tools must be installed natively.

```bash
apexpacks doctor
```

Example output (macOS):

```
apexpacks doctor

  docker      ✓ OK
              /usr/local/bin/docker  (Docker version 29.5.3)
              Container runtime (required on macOS; needed for health check tests)

  melange     ✓ OK
              /usr/local/bin/melange  (v0.19.0)
              APK package builder — needed natively for key generation; build runs in Docker on macOS

  apko        ✓ OK
              /usr/local/bin/apko  (v0.9.0)
              OCI image assembler — build runs in Docker on macOS, native on Linux

  grype       – missing (optional)
              CVE scanner — required for 'apexpacks scan'
              Install: brew install grype

Optional tools missing — 'apexpacks build' will work, but 'apexpacks scan' requires grype.
```

Exit codes: `0` when all required tools are present, `1` when any required tool is missing.

---

### `apexpacks detect [source-dir]`

Scans a source directory against all profiles and reports matches sorted by confidence. Identifies the runtime, framework, package manager, and language version. Writes results to `.apexpack/context.json` for use by subsequent `build` and `scan` commands.

```bash
apexpacks detect .
apexpacks detect /path/to/my-project
apexpacks detect . --profiles-dir /custom/profiles
```

Example output:

```
Detected 1 match(es) in .:

→ node           90%  framework: nextjs          version: 20       (matched: [package.json, pnpm-lock.yaml])

To build: apexpacks build .

Context: /path/to/project/.apexpack/context.json
```

| Flag | Default | Description |
|------|---------|-------------|
| `--profiles-dir` | built-in | Directory containing language profile YAML files |
| `--project-name` | dir name | Override the project name |
| `--git-url` | auto-detected | Repository URL written to context.json |
| `--git-branch` | auto-detected | Branch written to context.json |
| `--git-commit` | auto-detected | Commit SHA written to context.json |
| `--auto-patch` | from profile | Force auto-patch on (`true`) for this run |
| `--patch-persist` | from profile | Force patch-persist on (`true`) for this run |

---

### `apexpacks build [source-dir]`

Detects the language, loads the matching profile, resolves any framework or package-manager build overrides, generates `melange.yaml` and `apko.yaml`, then runs melange and apko to produce an OCI image tarball. Build caches are mounted as named Docker volumes on macOS so package managers reuse their download caches across builds. Writes build artifact paths to `.apexpack/context.json`.

```bash
# Auto-detect language and build
apexpacks build .

# Specify the image tag
apexpacks build . --tag ghcr.io/myorg/myapp:v1.0.0

# Skip detection — use a specific runtime profile
apexpacks build . --runtime java

# Preview generated configs without running tools
apexpacks build . --dry-run

# Override language version (e.g. Java 21 instead of auto-detected)
apexpacks build . --runtime java --lang-version 21

# Custom output directory
apexpacks build . --output /tmp/my-build

# Corporate proxy — trust an extra CA certificate
apexpacks build . --tls-extra-ca ~/corp-ca.pem

# Use docker as the melange sandbox backend (when bubblewrap is unavailable)
apexpacks build . --melange-runner docker

# Build tarball only, skip registry push
apexpacks build . --local

# Use an existing melange signing key
apexpacks build . --signing-key ~/.apexpack/melange.rsa

# Build for a specific architecture
apexpacks build . --arch aarch64
```

| Flag | Default | Description |
|------|---------|-------------|
| `--tag` / `-t` | `<project>:latest` | OCI image reference |
| `--runtime` | auto-detect | Skip detection, use this profile directly |
| `--version` | `0.0.1` | Version embedded in the APK package |
| `--lang-version` | auto-detected | Override the detected language version (e.g. `21` for Java 21) |
| `--project-name` | dir name | Override the project name |
| `--output` / `-o` | `.apexpack-output/` | Where configs and image tarball are written |
| `--dry-run` | `false` | Print generated configs, do not run tools |
| `--profiles-dir` | built-in | Directory containing language profiles |
| `--tls-extra-ca` | _(none)_ | Path to an extra CA cert (PEM) for corporate proxy environments (env: `APEXPACK_EXTRA_CA`) |
| `--arch` | host arch | Target architecture: `x86_64` or `aarch64` |
| `--melange-runner` | `bubblewrap` | Melange sandbox backend: `bubblewrap`, `docker`, or `qemu` |
| `--local` | `false` | Build tarball only, skip `apko publish` registry push |
| `--signing-key` | auto-generated | Path to an existing melange RSA private key (`.pub` must be alongside) |

---

### `apexpacks scan [output-dir]`

Scans the SBOM produced by `apexpacks build` for known CVEs using [grype](https://github.com/anchore/grype). Normalises SBOM version strings automatically before scanning. Updates the grype CVE database before each scan. Writes severity counts and scan result to `.apexpack/context.json`.

```bash
# Scan the last build (auto-reads SBOM path from context.json)
apexpacks scan

# Scan a specific output directory
apexpacks scan /path/to/.apexpack-output

# Scan a specific SBOM file
apexpacks scan --sbom /path/to/sbom-x86_64.spdx.json

# Fail if any HIGH or above CVE is found (for CI)
apexpacks scan --fail-on high

# Output SARIF for GitHub Code Scanning
apexpacks scan --format sarif --output results/

# Exit 0 even on failure (used in auto-patch flows)
apexpacks scan --soft-fail

# Write results to rescan_* fields after a patch cycle
apexpacks scan --rescan
```

| Flag | Default | Description |
|------|---------|-------------|
| `--sbom` | from context.json | Path to a specific SBOM file |
| `--fail-on` | _(none)_ | Exit 1 at this severity: `critical`, `high`, `medium`, `low` |
| `--format` | `table` | Output format: `table`, `json`, `sarif`, `cyclonedx` |
| `--output` / `-o` | _(none)_ | Write report file to this directory |
| `--source` | `.` | Source directory containing `.apexpack/context.json` |
| `--soft-fail` | `false` | Exit 0 even when CVEs found (for auto-patch pipeline flows) |
| `--rescan` | `false` | Write results to `rescan_*` fields in context.json (post-patch scan) |

---

### `apexpacks patch [output-dir]`

Compares installed package versions (from the last build SBOM) against the latest Wolfi index and cross-references with grype to identify which outdated packages have CVEs. With `--apply`, pins the patched versions in `apexpacks.yaml` in the project root.

```bash
# Show available updates and CVEs
apexpacks patch

# Apply patches — pins updated versions in apexpacks.yaml
apexpacks patch --apply

# Then rebuild to pick up the patches
apexpacks build .
```

| Flag | Default | Description |
|------|---------|-------------|
| `--sbom` | from context.json | Path to SBOM file |
| `--apply` | `false` | Update `apexpacks.yaml` with pinned patched versions |
| `--arch` | from context.json | Architecture to check against the Wolfi index |
| `--runtime` | _(all)_ | Only patch the profile for this runtime (e.g. `java`) |
| `--source` | `.` | Source directory containing `.apexpack/context.json` |

---

### `apexpacks profiles`

Lists all loaded language profiles with their detection rules and build dependencies.

```bash
# List all built-in profiles
apexpacks profiles

# List profiles from a custom directory
apexpacks profiles --profiles-dir ./my-profiles
```

#### `apexpacks profiles export <dir>`

Exports all built-in profiles to a directory so you can edit them.

```bash
apexpacks profiles export ./my-profiles
# Edit any .yaml file in my-profiles/
apexpacks build . --profiles-dir ./my-profiles
```

#### `apexpacks profiles new <name>`

Scaffolds a complete starter profile for a new language or framework.

```bash
apexpacks profiles new rust
apexpacks profiles new rust --output-dir ./my-profiles
```

The generated file includes placeholders for detect rules, build command, image packages, health check, and melange test pipeline. Edit it, then pass `--profiles-dir` to use it.

---

### `apexpacks normalize-sbom <sbom-path>`

Rewrites an SPDX SBOM to a temporary file with normalized `versionInfo` fields, stripping non-APK prefixes so grype can match packages accurately. The `scan` command runs this automatically — this command is for scripting and debugging.

```bash
NORMALIZED=$(apexpacks normalize-sbom sbom-x86_64.spdx.json)
grype sbom:$NORMALIZED
rm -f "$NORMALIZED"
```

---

### `apexpacks version`

```bash
apexpacks version
# apexpacks v0.2.0
```

---

## Health Checks

### Configuring a health check in a profile

Health checks are defined in the `image.health-check` section of a profile or `apexpacks.yaml`:

```yaml
image:
  health-check:
    http:
      port: 8080
      path: /health      # defaults to /
    interval: 30s        # how often to check (default: 30s)
    timeout: 5s          # per-check timeout (default: 5s)
    start-period: 10s    # grace period before checks start (default: 10s)
    retries: 3           # failures before unhealthy (default: 3)
```

For TCP services (databases, gRPC without HTTP, etc.):

```yaml
image:
  health-check:
    tcp:
      port: 5432
```

To disable the health check entirely:

```yaml
image:
  health-check:
    disabled: true
```

### What happens at build time

When a health check is configured, `apexpacks build` does four things:

1. **Package injection** — `wget` is automatically added to the image for HTTP checks; `busybox` for TCP checks. You do not need to declare them manually.
2. **`HEALTHCHECK` instruction** — the health check is embedded directly into the OCI image tar as a `HEALTHCHECK` layer, so it works natively with `docker run` and Kubernetes `livenessProbe`/`readinessProbe`.
3. **Boot check** — after build, the image is started in Docker for 5 seconds to confirm the container does not exit on startup. Container logs are printed.
4. **Endpoint probe** — HTTP GET or TCP connect is retried every 500ms for up to 30 seconds. The probe URL/port is taken from the health check config, with a per-framework default as fallback (e.g. FastAPI defaults to `GET /health` on port 8000).

Example build output:

```
  → Health Check Test
     image: myapp:latest
     --- docker output ---
     INFO:     Application startup complete.
     --- end output ---
     boot:   PASSED ✓  (container is running)
     probe:  HTTP GET http://localhost:8080/health  [config]
     probe:  PASSED ✓  (HTTP 200 in 2.3s)
```

### Framework-default probes

If no `health-check` is declared but the detected framework has a known default endpoint, the boot check still runs a probe automatically:

| Framework | Default probe |
|-----------|--------------|
| `fastapi` | `GET /health` on port `8000` |
| `django` | `GET /` on port `8000` |
| `flask` | `GET /` on port `5000` |
| `express` | `GET /` on port `3000` |
| `fastify` | `GET /health` on port `3000` |
| `nestjs` | `GET /` on port `3000` |
| `spring-boot` | `GET /actuator/health` on port `8080` |
| `gin` | `GET /` on port `8080` |

These are used only for the local boot-check probe — they do not inject a `HEALTHCHECK` into the image unless you also declare `image.health-check` in the profile.

### Kubernetes probes file

After a successful build, `apexpacks build` writes a `probes.yaml` file to the output directory. This file contains ready-to-paste Kubernetes `readinessProbe` and `livenessProbe` blocks derived from the health check configuration:

```yaml
# .apexpack-output/probes.yaml
readinessProbe:
  httpGet:
    path: /health
    port: 8080
  initialDelaySeconds: 10
  periodSeconds: 30
  timeoutSeconds: 5
  failureThreshold: 3
livenessProbe:
  httpGet:
    path: /health
    port: 8080
  initialDelaySeconds: 10
  periodSeconds: 30
  timeoutSeconds: 5
  failureThreshold: 3
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
    │
    ▼
Health check        Boot check + endpoint probe (when health-check declared)
    │
    ▼
probes.yaml         Kubernetes readiness/liveness probe YAML
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
  content:
    - file: package.json
      contains: "\"next\""
      boost-confidence: 0.05
      framework: nextjs
    - file: package.json
      contains: "\"express\""
      boost-confidence: 0.03
      framework: express

  confidence: 0.85   # base confidence score (0.0–1.0)

# ── Build (feeds into melange.yaml) ────────────────────────────────────────
build:
  dependencies:        # APK packages in the BUILD environment (not in the final image)
    - nodejs
    - npm
    - git
  command: |
    npm ci --prefer-offline
    npm run build --if-present
    mkdir -p ${{targets.destdir}}/app
    cp -r . ${{targets.destdir}}/app/
    rm -rf ${{targets.destdir}}/app/node_modules
    cd ${{targets.destdir}}/app && npm ci --omit=dev --prefer-offline
  env:
    NODE_ENV: "production"
    NPM_CONFIG_CACHE: "/home/build/.npm"
  caches:              # paths persisted as named Docker volumes between builds (macOS)
    - /home/build/.npm

  # Framework/package-manager overrides.
  # Lookup order: {framework}-{packageManager} → {packageManager} → {framework} → default
  frameworks:
    pnpm:
      dependencies:
        - nodejs
        - npm
        - git
      command: |
        npm install -g pnpm
        pnpm install --frozen-lockfile
        pnpm run build --if-present
        mkdir -p ${{targets.destdir}}/app
        cp -r . ${{targets.destdir}}/app/
        cd ${{targets.destdir}}/app && pnpm install --frozen-lockfile --prod
      env:
        NODE_ENV: "production"
        PNPM_HOME: "/home/build/.local/share/pnpm"
      caches:
        - /home/build/.local/share/pnpm/store

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
  health-check:
    http:
      port: 3000
      path: /health
    interval: 30s
    timeout: 5s
    start-period: 10s
    retries: 3

# ── Melange test pipeline ───────────────────────────────────────────────────
test:
  packages:
    - busybox
  pipeline:
    - runs: |
        test -f /app/server.js || { echo "ERROR: /app/server.js not found"; exit 1; }

# ── CVE Auto-patch ──────────────────────────────────────────────────────────
scan:
  auto-patch: false
  patch-persist: false
```

---

### The `scan` section

| Field | Default | Description |
|-------|---------|-------------|
| `auto-patch` | `false` | When `true`, a CVE scan failure is treated as soft — the pipeline continues to apply patches and rebuild instead of failing hard |
| `patch-persist` | `false` | When `true`, the updated profile YAML (with pinned package versions) is committed back to git after patching so future builds start already fixed |

---

### Detection: framework vs package manager

**Framework** is set by the first matching `content` rule. **Package manager** is set by the first matching `package-managers` rule. They combine during build override resolution:

```
Detected framework:        nextjs
Detected package manager:  pnpm

Override lookup order:
  1. "nextjs-pnpm"   → not in frameworks map
  2. "pnpm"          → found! use pnpm command and caches   ✓
  3. "nextjs"        → (skipped)
  4. default         → (skipped)
```

For Java, build tool (Maven vs Gradle) is encoded in the framework name because it changes the command entirely:

```yaml
content:
  - file: pom.xml
    contains: "spring-boot"
    framework: spring-boot          # Maven
  - file: build.gradle
    contains: "spring-boot"
    framework: spring-boot-gradle   # Gradle
```

---

### Build caching

Each profile and framework entry can declare `caches` — paths inside the build container persisted as named Docker volumes on macOS:

```yaml
build:
  caches:
    - /home/build/.m2/repository   # Maven local repo

  frameworks:
    spring-boot-gradle:
      caches:
        - /home/build/.gradle      # replaces build.caches for this framework
```

Framework-level `caches` replace (not append to) the top-level `build.caches`.

---

### Procfile support

If a project has a `Procfile` with a `web:` process and the profile has no explicit `image.entrypoint`, apexpacks parses the Procfile and uses the `web:` command as the container entrypoint:

```
# Procfile
web: node dist/server.js
```

The profile's `image.entrypoint` always takes precedence. Procfile is only used as a fallback.

---

### Per-project `apexpacks.yaml` overrides

Place an `apexpacks.yaml` in the project root to override or extend the detected profile. Only set what you need to change — everything else is inherited.

```yaml
# apexpacks.yaml
runtime: golang          # optional — overrides auto-detection

build:
  command: |
    mkdir -p ${{targets.destdir}}/usr/bin
    go build -o ${{targets.destdir}}/usr/bin/{APP_NAME} ./cmd/myapp
  env:
    CGO_ENABLED: "1"
  dependencies:
    - sqlite-libs

image:
  packages:
    - sqlite-libs
  env:
    DATABASE_PATH: "/data/app.db"
  health-check:
    http:
      port: 8080
      path: /healthz
```

`{APP_NAME}` is substituted with the sanitized project name at build time.

---

### Adding a New Language Profile

1. `apexpacks profiles new rust --output-dir ./my-profiles` to scaffold a starter file
2. Define `detect`, `build`, `image` sections
3. Add `package-managers` rules for languages with multiple build tools
4. Add `frameworks` entries only for cases that need a different command, deps, env, or caches
5. Add `test.pipeline` to verify build artifacts (run by `melange test`)
6. Add a `scan` block (safe default: `auto-patch: false`)
7. `apexpacks profiles --profiles-dir ./my-profiles` to verify it loads
8. `apexpacks detect /path/to/sample --profiles-dir ./my-profiles` to test detection
9. `apexpacks build /path/to/sample --profiles-dir ./my-profiles --dry-run` to verify generated configs

---

## How Detection Confidence Works

```
Base confidence (from detect.confidence)         e.g. 0.85
+ boost from each matching content rule          e.g. +0.05 (spring-boot found in pom.xml)
+ boost from each matching content rule          e.g. +0.02 (additional signal)
──────────────────────────────────────────────────────────
Final confidence                                 e.g. 0.92  (capped at 1.0)
```

Package manager rules do not affect confidence — they only set `PackageManager`. When multiple profiles match, results are sorted highest-confidence first. Use `--runtime` to override.

---

## Project Structure

```
apexpack/
│
├── cmd/apexpacks/
│   ├── main.go              rootCmd + version variable
│   ├── cmd_build.go         buildCmd — all build flags and orchestration
│   ├── cmd_detect.go        detectCmd — language detection + context.json
│   ├── cmd_scan.go          scanCmd — grype CVE scanning
│   ├── cmd_patch.go         patchCmd — Wolfi package update checking + pinning
│   ├── cmd_profiles.go      profilesCmd, profilesExportCmd, profilesNewCmd,
│   │                        normalizeSBOMCmd, versionCmd
│   └── util.go              resolveProfilesDir, findTool, buildArch, git helpers
│
├── internal/
│   ├── types/
│   │   └── types.go         ALL data structures — Profile, DetectResult, BuildPlan,
│   │                        BuildOptions, MelangeConfig, ApkoConfig, HealthCheckConfig, etc.
│   │
│   ├── profile/
│   │   └── profile.go       LoadAll(), LoadProjectConfig(), MergeProjectConfig(),
│   │                        GetByRuntime(), ExportEmbedded()
│   │
│   ├── detect/
│   │   └── detect.go        Run() → []DetectResult sorted by confidence
│   │                        Best() → *DetectResult
│   │                        LanguageVersion() → auto-detected version string
│   │
│   ├── build/
│   │   ├── build.go         Public API — Plan(), Run(), MarshalMelange(), MarshalApko()
│   │   │
│   │   ├── helpers/         Pure utilities (no I/O, no process calls)
│   │   │   ├── version.go   LangVersionToken, ResolveVersion, ValidateRuntimeVersion,
│   │   │   │                Vsub, VsubSlice, VsubMap
│   │   │   ├── util.go      SanitizeImageName, ApplyProjectTemplates, ReadProcfileCmd,
│   │   │   │                CacheVolumeName, ReadNodeEntrypoint
│   │   │   ├── override.go  ResolveOverride — framework+pm fallback lookup
│   │   │   └── probedefaults.go  FrameworkProbeDefaults — per-framework probe path/port
│   │   │
│   │   ├── templates/       Embedded build config templates (baked into binary)
│   │   │   ├── template.go  LoadMavenTemplate, LoadNuGetTemplate, LoadGradleTemplate
│   │   │   ├── maven/       maven_settings.xml
│   │   │   ├── nuget/       nuget_config.xml
│   │   │   └── gradle/      gradle_init.gradle
│   │   │
│   │   ├── hooks/           Language-specific config patches
│   │   │   ├── hook.go      LanguageHook interface + registry + Get()
│   │   │   ├── hook_java.go     JDK version, JAVA_HOME, Maven/Gradle injection
│   │   │   ├── hook_dotnet.go   .NET version substitution, NuGet.Config
│   │   │   ├── hook_node.go     Node version, package.json entrypoint detection
│   │   │   ├── hook_python.go   Python version substitution
│   │   │   ├── hook_go.go       Go version, GOPATH/cache env
│   │   │   └── hook_webserver.go  nginx config placement, runtime dirs
│   │   │
│   │   ├── config/          Config struct generation (no processes, no hooks)
│   │   │   └── config.go    BuildMelangeConfig, BuildApkoConfig, BuildHealthCheck,
│   │   │                    EnsurePackage, MarshalYAML
│   │   │
│   │   ├── runner/          External tool execution (all I/O side-effects)
│   │   │   ├── runner.go    RunMelange, RunMelangeTest, RunApko
│   │   │   ├── exec.go      runTool helpers, melangeArch, archToDockerPlatform
│   │   │   ├── ca.go        readCACerts, mergeCABundles
│   │   │   └── healthcheck.go  InjectHealthCheckIntoTar, RunHealthCheckTest
│   │   │
│   │   └── probes/          Kubernetes probe YAML generation
│   │       └── probes.go    EmitProbesYAML
│   │
│   ├── apexctx/             context.json read/write (pipeline state handoff)
│   │
│   └── patch/
│       └── patch.go         Check() — Wolfi index comparison
│                            ApplyToProfile() — version pin updates
│                            NormalizeSBOMFile() — grype SBOM pre-processing
│
├── profiles/                Language profile YAML files (embedded into binary at build time)
│   ├── golang.yaml
│   ├── java.yaml
│   ├── dotnet.yaml
│   ├── node.yaml
│   ├── python.yaml
│   └── webserver.yaml
│
├── .goreleaser.yaml         Cross-platform binary release config (linux/darwin × amd64/arm64)
│
├── apexpacks.yaml           Per-project overrides for building apexpack itself
│
├── rebuild-image.sh         Rebuilds apexpack:latest and loads it into the kind cluster
│
└── tekton/
    ├── install/             Tekton install manifests
    ├── tasks/               apexpack-detect, apexpack-build, apexpack-scan,
    │                        apexpack-patch, git-clone, crane-copy
    ├── pipelines/           build-and-push Pipeline
    ├── pipelinerun/         Example PipelineRun (spring-petclinic)
    └── config/              Supporting cluster config
```

---

## Development Guide

### Prerequisites

| Tool | macOS | Ubuntu |
|------|-------|--------|
| Go 1.25+ | `brew install go` | `sudo apt install golang-go` (or use [go.dev](https://go.dev/dl/)) |
| Docker | `brew install colima docker && colima start` (or Docker Desktop) | `sudo apt install docker.io` |
| grype (for scan) | `brew install grype` | `curl -sSfL https://raw.githubusercontent.com/anchore/grype/main/install.sh \| sh -s -- -b /usr/local/bin` |
| melange | `brew install melange` | `curl -sL https://github.com/chainguard-dev/melange/releases/latest/download/melange_linux_amd64.tar.gz \| tar xz && sudo mv melange /usr/local/bin/` |
| apko | `brew install apko` | `curl -sL https://github.com/chainguard-dev/apko/releases/latest/download/apko_linux_amd64.tar.gz \| tar xz && sudo mv apko /usr/local/bin/` |

On **macOS**, melange and apko are installed via Homebrew. The actual build and image assembly run inside Docker automatically — Docker is required.

After installing, run `apexpacks doctor` to verify everything is reachable:

### Clone and build

```bash
git clone https://github.com/apexpack/apexpack
cd apexpack
go build -o bin/apexpacks ./cmd/apexpacks
./bin/apexpacks version
```

### Run tests

```bash
go test ./...
```

### Run GoReleaser locally (dry run)

```bash
# Install goreleaser
brew install goreleaser      # macOS
# or: go install github.com/goreleaser/goreleaser/v2@latest

# Snapshot build (no git tag required)
goreleaser release --snapshot --clean

# Binaries are in dist/
ls dist/
# apexpacks_darwin_arm64.tar.gz
# apexpacks_darwin_amd64.tar.gz
# apexpacks_linux_amd64.tar.gz
# apexpacks_linux_arm64.tar.gz
# checksums.txt
```

### Releasing a new version

1. Create and push a git tag: `git tag v0.2.0 && git push origin v0.2.0`
2. The `release.yaml` GitHub Actions workflow triggers GoReleaser automatically
3. Binaries appear on the GitHub Releases page within a few minutes

### Adding a language hook

Language hooks (`internal/build/hooks/hook_<lang>.go`) handle runtime-specific logic — JAVA_HOME path fixes, NuGet.Config injection, nginx directory permissions. Each hook implements the `LanguageHook` interface:

```go
type LanguageHook interface {
    PatchMelange(cfg *types.MelangeConfig, p *types.Profile, opts types.BuildOptions) error
    PatchApko(cfg *types.ApkoConfig, p *types.Profile, opts types.BuildOptions) error
}
```

Steps to add a new hook:
1. Create `internal/build/hooks/hook_<lang>.go` implementing `LanguageHook`
2. Register it in `hook.go`: `registry["rust"] = &rustHook{}`
3. Add a profile YAML with the matching `runtime:` value
4. Add tests in `hook_<lang>_test.go` using `package hooks` to access unexported helpers

### Package dependency rules

The subpackages have a strict no-cycle dependency order:

```
build.go
  ├── config/      ← imports types/, helpers/
  ├── hooks/       ← imports types/, helpers/, templates/
  ├── runner/      ← imports types/
  └── probes/      ← imports types/, helpers/

helpers/    — leaf, imports types/ only
templates/  — leaf, no internal imports
```

Nothing imports from `cmd/`. All shared data structures live in `internal/types/types.go`.

---

## Corporate Proxy Environments

In networks where a TLS-intercepting proxy (Zscaler, Blue Coat, etc.) replaces certificates, the melange build container will fail to reach `packages.wolfi.dev`:

```
failed to verify the x509 cert: signed by unknown authority
```

**Step 1 — get the corporate CA certificate (PEM format)**

```bash
# macOS — export from system Keychain
security find-certificate -a -p /Library/Keychains/System.keychain > ~/corp-ca.pem

# Linux
cp /usr/local/share/ca-certificates/corporate.crt ~/corp-ca.pem
```

**Step 2 — pass it to apexpack**

```bash
# Via flag
apexpacks build . --tls-extra-ca ~/corp-ca.pem

# Via environment variable — set once in your shell profile
export APEXPACK_EXTRA_CA=~/corp-ca.pem
apexpacks build .
```

The flag takes precedence over the environment variable. The corporate cert is added alongside the container's existing trust store, not instead of it.

---

## Tekton Pipeline Integration

apexpacks ships Tekton Tasks and a Pipeline that clone the source, detect the language, build the image, scan for CVEs — and when CVEs are found, automatically patch and rebuild before pushing.

### Installing Tekton

```bash
kubectl apply -f tekton/install/tekton-pipeline.yaml
kubectl apply -f tekton/install/tekton-dashboard.yaml
```

> The bundled `tekton-pipeline.yaml` sets `coschedule: disabled` in `feature-flags`. This is required because the build task binds two PVCs (`source` and `output`) simultaneously, which is incompatible with the default `coschedule: workspaces` mode.

> **Privileged build pods:** the `apexpack-build` task runs with `privileged: true` and `runAsUser: 0`. Both are required: melange uses bubblewrap for build sandboxing, and bubblewrap needs user namespace mappings which requires effective capabilities retained only at uid 0.

### Creating the PVCs

```bash
kubectl apply -f - <<'EOF'
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: apexpack-source
spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 2Gi
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: apexpack-profiles
spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 100Mi
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: apexpack-output
spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 2Gi
EOF
```

### Applying Tasks and Pipeline

```bash
kubectl apply -f tekton/tasks/
kubectl apply -f tekton/pipelines/
```

### Running the Pipeline

```bash
kubectl create -f tekton/pipelinerun/pipelinerun.yaml
```

Edit `GIT_URL`, `IMAGE`, and `GIT_REVISION` in the PipelineRun:

```yaml
apiVersion: tekton.dev/v1
kind: PipelineRun
metadata:
  generateName: apexpack-build-
spec:
  pipelineRef:
    name: apexpack-build-and-push
  params:
    - name: GIT_URL
      value: https://github.com/myorg/myapp
    - name: GIT_REVISION
      value: main
    - name: IMAGE
      value: ghcr.io/myorg/myapp:v1.0.0
    - name: FAIL_ON
      value: high
  workspaces:
    - name: source
      persistentVolumeClaim:
        claimName: apexpack-source
    - name: profiles
      persistentVolumeClaim:
        claimName: apexpack-profiles
    - name: output
      persistentVolumeClaim:
        claimName: apexpack-output
```

For quick testing without a registry: `ttl.sh/myapp:1h`

### Pipeline Steps

| Step | Task | Description |
|------|------|-------------|
| 1 | `git-clone` | Clone the source repo into the `source` workspace |
| 2 | `apexpack-detect` | Detect language and framework; seed profiles from baked-in image; emit `RUNTIME`, `AUTO_PATCH`, `PATCH_PERSIST` results |
| 3 | `apexpack-build` | Build OCI image with melange + apko; emit `IMAGE_TARBALL`, `SBOM_PATH` results |
| 4 | `apexpack-scan` | Scan SBOM for CVEs; when `AUTO_PATCH=true`, exits 0 on failure (soft-fail) so pipeline continues |
| 5 | `apexpack-patch` | _(when scan failed AND AUTO_PATCH=true)_ Run `apexpack patch --apply`; optionally commit back to git |
| 6 | `apexpack-build` | _(when scan failed AND AUTO_PATCH=true)_ Rebuild with patched profile |
| 7 | `crane-copy` | Push image tarball to registry |

### CVE Auto-patch Loop

```
scan (CVEs found, soft-fail)
    │
    ▼
patch (apexpacks patch --apply → pins updated Wolfi packages in apexpacks.yaml)
    │  optionally: git commit + push if patch-persist: true
    ▼
rebuild (apexpacks build with patched profile → clean image)
    │
    ▼
push (crane push → registry)
```

Enable per-profile:

```yaml
# profiles/java.yaml
scan:
  auto-patch: true
  patch-persist: false
```

Override per-run without changing the profile:

```yaml
params:
  - name: AUTO_PATCH
    value: "true"
  - name: PATCH_PERSIST
    value: "true"
```

### The `apexpack:latest` Tool Image

All pipeline tasks run inside `apexpack:latest` — a self-built image bundling the CLI together with melange, apko, grype, busybox, and git. The image builds itself using `apexpacks.yaml` at the repo root.

To rebuild and reload into a kind cluster:

```bash
./rebuild-image.sh
```

---

## Acknowledgements

- **[melange](https://github.com/chainguard-dev/melange)** — APK package builder from source, by Chainguard
- **[apko](https://github.com/chainguard-dev/apko)** — OCI image assembler from APK packages, by Chainguard
- **[Wolfi](https://github.com/wolfi-dev/os)** — supply chain-hardened Linux undistro, by Chainguard
- **[cobra](https://github.com/spf13/cobra)** — CLI framework for Go
- **[goreleaser](https://github.com/goreleaser/goreleaser)** — cross-platform binary release automation
- **[grype](https://github.com/anchore/grype)** — vulnerability scanner for container images, by Anchore
