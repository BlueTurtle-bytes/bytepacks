# apexpacks.yaml — Project Configuration Guide

`apexpacks.yaml` is an optional file you place in your project root to customise
how apexpack builds and packages your application. It sits on top of the language
profile — you only specify what you want to change; everything else keeps its
profile default.

---

## When do you need it?

You need an `apexpacks.yaml` when the detected profile does not fully match your
project — for example:

- Your app needs an extra runtime package (e.g. `libpq` for Postgres, `ffmpeg`)
- You need a build-time environment variable (e.g. `CGO_ENABLED=1`)
- You have extra source directories to copy into the image
- You want to override the health-check path or port
- You want to pin the language version rather than let apexpack detect it

---

## Creating the file

Place `apexpacks.yaml` in the **project root** — the same folder that contains
your `.csproj`, `go.mod`, `package.json`, or `pom.xml`.

For monorepos with `--project-subpath`, place it inside the subpath directory:

```
MyRepo/
  Absa.Spear.MandateCalculator.Web/
    apexpacks.yaml      ← here
    appsettings.json
    MandateCalculator.csproj
  MyRepo.sln
```

---

## Full schema

```yaml
# Override auto-detected runtime (optional).
# Values: golang, dotnet, node, python, java
runtime: dotnet

# Override auto-detected language version (optional).
# Useful when global.json or .nvmrc is absent or too broad.
language_version: "8"

# ── Build overrides ──────────────────────────────────────────────────────────
build:
  # Extra APK packages installed in the build container (not in the final image).
  dependencies:
    - cmake
    - protobuf-dev

  # Environment variables set during the build step.
  env:
    CGO_ENABLED: "1"
    MY_BUILD_FLAG: "true"

  # Extra source directories to copy into /app after publish.
  # The profile already includes: configs, config, templates, wwwroot, scripts, sql.
  # Add any project-specific directories here.
  copy_source_dirs:
    - resources
    - migrations

  # Fully replace the profile's build command (advanced — use sparingly).
  command: |
    dotnet publish -c Release -o ${{targets.destdir}}/app

# ── Image (runtime) overrides ────────────────────────────────────────────────
image:
  # Extra APK packages added to the runtime image.
  packages:
    - libpq
    - ffmpeg

  # Environment variables set in the running container.
  env:
    TZ: Africa/Johannesburg
    ASPNETCORE_ENVIRONMENT: Production

  # Override the working directory inside the container.
  work-dir: /app

  # Override the container entrypoint binary.
  entrypoint: /usr/bin/dotnet

  # Override the default CMD arguments.
  cmd:
    - /app/app.dll

  # Extra filesystem paths to create in the image.
  paths:
    - path: /data
      type: directory
      permissions: 0755
      uid: 1001
      gid: 1001

  # Override the health-check (set disabled: true for non-HTTP apps).
  health-check:
    http:
      path: /healthz
      port: 8080
    interval: 30s
    timeout: 5s
    start-period: 20s
    retries: 3

# ── Scan overrides ───────────────────────────────────────────────────────────
scan:
  auto-patch: false
  patch-persist: false

# ── Test pipeline override ───────────────────────────────────────────────────
test:
  pipeline:
    - runs: |
        test -f /app/app.dll || { echo "ERROR: app.dll missing"; exit 1; }
        echo "✓ app.dll present"
```

---

## Searching for packages

### Wolfi (default)

Search the Wolfi package index:

```bash
# Search by name
curl -s "https://packages.wolfi.dev/os/x86_64/APKINDEX.tar.gz" \
  | tar -xzO APKINDEX \
  | grep "^P:" \
  | grep -i <term>

# Examples
grep -i postgres
grep -i ffmpeg
grep -i "dotnet"
```

Or browse online at **packages.wolfi.dev**.

### Alpine (when using `--distro alpine`)

```bash
# Community + main repos
curl -s "https://dl-cdn.alpinelinux.org/alpine/edge/community/x86_64/APKINDEX.tar.gz" \
  | tar -xzO APKINDEX | grep "^P:" | grep -i <term>

curl -s "https://dl-cdn.alpinelinux.org/alpine/edge/main/x86_64/APKINDEX.tar.gz" \
  | tar -xzO APKINDEX | grep "^P:" | grep -i <term>
```

### Tips

- **Runtime packages** go under `image.packages` — these end up in the final image.
- **Build-only packages** go under `build.dependencies` — installed during compile, not shipped.
- **Pin a version** when you need a specific release: `libpq=16.3-r0`. Omit the version to always get the latest.
- If a package exists in Wolfi with a different name than Alpine, the profile's
  `distro:` block handles that automatically — you only need to add the Wolfi name
  under `image.packages`.

---

## Common examples

### Add a timezone

```yaml
image:
  env:
    TZ: Africa/Johannesburg
```

> `tzdata` is already included in the dotnet profile. For other runtimes add
> `tzdata` to `image.packages` as well.

### Add a native library (e.g. librdkafka)

```yaml
image:
  packages:
    - librdkafka
```

### Enable CGO for Go

```yaml
build:
  dependencies:
    - gcc
    - musl-dev
  env:
    CGO_ENABLED: "1"
```

### Copy extra source directories (dotnet)

```yaml
build:
  copy_source_dirs:
    - migrations
    - email-templates
```

### Disable health-check for a background worker

```yaml
image:
  health-check:
    disabled: true
```

### Force a specific .NET version

```yaml
language_version: "8"
```
