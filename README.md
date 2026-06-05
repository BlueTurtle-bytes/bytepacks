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

**Corporate / proxy environments:** if your network uses TLS inspection (Zscaler, etc.), see the [Corporate Proxy Environments](#corporate-proxy-environments) section before running `go build`.

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

# ── CVE Auto-patch ──────────────────────────────────────────────────────────
scan:
  auto-patch: false      # when true, CVE failures in the pipeline trigger auto-patch + rebuild
  patch-persist: false   # when true, patched profiles are committed back to git
```

---

### The `scan` section

Each profile carries a `scan` block that controls CVE auto-patching behaviour in the Tekton pipeline:

| Field | Default | Description |
|-------|---------|-------------|
| `auto-patch` | `false` | When `true`, a CVE scan failure is treated as soft — the pipeline continues to apply patches and rebuild instead of failing hard |
| `patch-persist` | `false` | When `true`, the updated profile YAML (with pinned package versions) is committed back to git after patching so future builds start already fixed |

These are read from the matched profile by the `apexpack-detect` Tekton task and emitted as pipeline results (`AUTO_PATCH`, `PATCH_PERSIST`). Pipeline params `AUTO_PATCH` and `PATCH_PERSIST` can override the profile values for a single run.

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
    go build -o ${{targets.destdir}}/usr/bin/{APP_NAME} ./cmd/myapp
  env:                   # merged on top of profile build.env
    CGO_ENABLED: "1"
  dependencies:          # appended to profile build.dependencies
    - sqlite-libs

image:
  packages:              # appended to profile image.packages
    - sqlite-libs
  env:                   # merged on top of profile image.env
    DATABASE_PATH: "/data/app.db"
```

`{APP_NAME}` is substituted with the project name at build time (derived from the source directory name or `--project-name`). Use it in `build.command` and `image.entrypoint` to avoid hardcoding binary names — every project automatically gets a binary and entrypoint named after itself.

#### Go projects with `cmd/` layout

Go projects where `main.go` lives in `cmd/<name>/` (not at the root) need this override, since the default `find`-based command would locate the right `main.go` but may resolve the wrong package path in monorepos:

```yaml
# apexpack.yaml
runtime: golang
build:
  command: |
    mkdir -p ${{targets.destdir}}/usr/bin
    go build -o ${{targets.destdir}}/usr/bin/{APP_NAME} ./cmd/myapp
```

---

### Adding a New Language Profile

1. Create `profiles/<runtime>.yaml`
2. Define `runtime`, `detect`, `build`, `image` (all required)
3. Add `package-managers` rules if the language supports multiple build tools
4. Add `frameworks` entries only for cases that need different `command`, `dependencies`, `env`, or `caches`
5. Add a `scan` block to configure CVE auto-patch behaviour (optional, defaults to disabled)
6. Run `apexpack profiles` to verify it loads
7. Run `apexpack detect /path/to/sample-project` to test detection
8. Run `apexpack build /path/to/sample-project --dry-run` to verify generated configs

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

scan:
  auto-patch: false
  patch-persist: false
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
│   │   └── types.go         ALL data structures — Profile, DetectResult, BuildPlan, ScanConfig, etc.
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
│       └── patch.go         Checks Wolfi index for updates, applies version pins to profiles.
│
├── profiles/                Language profile YAML files (baked into apexpack:latest at build time)
│   ├── golang.yaml
│   ├── java.yaml
│   ├── dotnet.yaml
│   ├── node.yaml
│   ├── python.yaml
│   └── webserver.yaml
│
├── apexpack.yaml            Per-project overrides for building apexpack itself
│                            (binary name, extra packages: busybox, git, melange, apko, grype)
│
├── rebuild-image.sh         Rebuilds apexpack:latest and loads it into the kind cluster
│
└── tekton/
    ├── install/             Tekton install manifests (pipeline + dashboard)
    ├── tasks/               apexpack-detect, apexpack-build, apexpack-scan,
    │                        apexpack-patch, git-clone, crane-copy
    ├── pipelines/           build-and-push Pipeline
    ├── pipelinerun/         Example PipelineRun (spring-petclinic)
    └── config/              Supporting cluster config
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

**7. Profiles are baked into the `apexpack:latest` image**
Profiles are embedded at `/etc/apexpack/profiles/` when the image is built (`cp profiles/*.yaml` runs as part of the Go build step in `apexpack.yaml`). The Tekton detect task seeds the profiles PVC from the image on every run — no manual `kubectl cp` or separate profiles repository is needed. Updating profiles requires only a rebuild of `apexpack:latest`.

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

apexpack ships Tekton Tasks and a Pipeline that clone the source, detect the language, build the image, scan for CVEs — and when CVEs are found, automatically patch and rebuild before pushing.

### Installing Tekton

```bash
kubectl apply -f tekton/install/tekton-pipeline.yaml
kubectl apply -f tekton/install/tekton-dashboard.yaml
```

> The bundled `tekton-pipeline.yaml` sets `coschedule: disabled` in `feature-flags`. This is required because the build task binds two PVCs (`source` and `output`) simultaneously, which is incompatible with the default `coschedule: workspaces` mode. On a single-node cluster (kind, k3d) this has no scheduling impact.

> **Privileged build pods:** the `apexpack-build` task runs with `privileged: true` and `runAsUser: 0`. Both are required: melange uses bubblewrap for build sandboxing, and bubblewrap needs to set up user namespace mappings which requires effective capabilities — capabilities that are only retained when the process runs as uid 0, even inside a privileged pod (Wolfi images default to a non-root user).

### Creating the PVCs

The pipeline uses three PVCs. Create them once:

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

`tekton/pipelinerun/pipelinerun.yaml` is a ready-to-use example that builds `spring-petclinic`. Edit `GIT_URL`, `IMAGE`, and `GIT_REVISION` before running:

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

For quick testing without a registry account, use `ttl.sh` (anonymous, ephemeral):
```yaml
    - name: IMAGE
      value: ttl.sh/myapp:1h
```

### Pipeline Steps

| Step | Task | Description |
|------|------|-------------|
| 1 | `git-clone` | Clone the source repo into the `source` workspace |
| 2 | `apexpack-detect` | Detect language and framework; seed profiles from the baked-in image; emit `RUNTIME`, `AUTO_PATCH`, `PATCH_PERSIST` results |
| 3 | `apexpack-build` | Build OCI image with melange + apko; emit `IMAGE_TARBALL`, `SBOM_PATH` results |
| 4 | `apexpack-scan` | Scan SBOM for CVEs with grype; when `AUTO_PATCH=true` exits 0 on failure (soft-fail) so the pipeline continues |
| 5 | `apexpack-patch` | _(when scan failed AND AUTO_PATCH=true)_ Run `apexpack patch --apply` to pin patched versions in the profile YAML; optionally commit back to git when `PATCH_PERSIST=true` |
| 6 | `apexpack-build` | _(when scan failed AND AUTO_PATCH=true)_ Rebuild the image using the patched profile |
| 7 | `crane-copy` | Push the image tarball to the registry (runs whether or not steps 5–6 were skipped) |

### CVE Auto-patch Loop

The pipeline implements a detect-patch-rebuild loop controlled entirely by the language profile:

```
scan (CVEs found, soft-fail)
    │
    ▼
patch (apexpack patch --apply → pins updated Wolfi packages in profile YAML)
    │  optionally: git commit + push if patch-persist: true
    ▼
rebuild (apexpack build with patched profile → clean image)
    │
    ▼
push (crane push → registry)
```

**Enabling auto-patch for a language:** set `scan.auto-patch: true` in the profile file:

```yaml
# profiles/java.yaml
scan:
  auto-patch: true      # CVE failures trigger patch + rebuild
  patch-persist: false  # set true to commit pinned versions back to git
```

**Overriding per-run** (without changing the profile):

```yaml
# pipelinerun.yaml
params:
  - name: AUTO_PATCH
    value: "true"
  - name: PATCH_PERSIST
    value: "true"
```

**How soft-fail works:** when `AUTO_PATCH=true`, the scan task writes `SCAN_RESULT=fail` to its result but exits with code 0 instead of 1. The patch and rebuild tasks have `when:` conditions that check both `SCAN_RESULT=fail` and `AUTO_PATCH=true` — so they only run when there are actual CVEs to fix. The push task uses `runAfter: [rebuild]` and runs regardless of whether rebuild was skipped.

### The `apexpack:latest` Tool Image

All pipeline tasks run inside `apexpack:latest` — a self-built image that bundles the CLI together with melange, apko, grype, busybox, and git. On Linux (inside pods) these tools are called natively, not via Docker.

The image builds itself using `apexpack.yaml` at the repo root:

```yaml
runtime: golang
build:
  command: |
    mkdir -p ${{targets.destdir}}/usr/bin
    mkdir -p ${{targets.destdir}}/etc/apexpack/profiles
    go build -o ${{targets.destdir}}/usr/bin/{APP_NAME} ./cmd/apexpack
    cp profiles/*.yaml ${{targets.destdir}}/etc/apexpack/profiles/
image:
  packages:
    - busybox      # /bin/sh for Tekton script steps
    - git          # required by the patch persist step
    - melange
    - apko
    - bubblewrap   # melange's sandbox runner
    - grype
```

The `detect` task seeds the profiles PVC from `/etc/apexpack/profiles/` on every run, so updating profiles only requires rebuilding the image — no manual file copying to the cluster.

To rebuild and reload into a kind cluster:

```bash
./rebuild-image.sh
```

---

## Contributing

### Adding a language profile

1. Create `profiles/<runtime>.yaml`
2. Define `runtime`, `detect`, `build`, `image` (all required)
3. Add `package-managers` rules if the language has multiple build tools (pnpm, uv, etc.)
4. Add `frameworks` entries for any cases that need a different `command`, `dependencies`, `env`, or `caches`
5. Add a `scan` block (`auto-patch: false`, `patch-persist: false` is a safe default)
6. Run `apexpack profiles` to verify it loads
7. Run `apexpack detect /path/to/sample-project` to test detection
8. Run `apexpack build /path/to/sample-project --dry-run` to verify generated configs
9. Rebuild `apexpack:latest` so the new profile is baked in: `./rebuild-image.sh`

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
- **[crane](https://github.com/google/go-containerregistry)** — OCI registry client for pushing images, by Google
