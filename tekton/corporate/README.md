# Corporate Tekton Pipelines — Deployment Guide

Deploys the apexpack corporate pipelines into your own namespace on the shared cluster.
These pipelines replace `install-external-artifacts` + `build-push-image` ClusterTasks
with apexpack's melange + apko build chain.

---

## Folder structure

```
tekton/corporate/
├── tasks/
│   ├── apexpack-build.yaml      # apexpack-build-corporate Task
│   ├── crane-push.yaml          # crane-push-corporate Task
│   └── apexpack-self-build.yaml # apexpack self-build Task (for the CI pipeline)
├── pipeline.yaml                # Simple pipeline: build → push
├── pipeline-full.yaml           # Full pipeline: build → scan → patch → rebuild → rescan → push
├── pipeline-self.yaml           # CI pipeline for the apexpack project itself
└── README.md                    # This file
```

The apexpack detect/scan/patch Tasks live in `tekton/tasks/` and must also be applied.

---

## Prerequisites

- `kubectl` configured against your corporate cluster with access to your namespace
- `tkn` CLI installed (optional but useful)
- An existing apexpack image available at your Artifactory registry
  (`APEXPACK_IMAGE` param — used as the build runner)
- The following ClusterTasks already installed on the cluster (they are):
  - `git-clone-v1-0`
  - `gitleaks`
  - `build-version-v1-0`

---

## 1. Set your namespace

```bash
NAMESPACE=<your-namespace>
kubectl config set-context --current --namespace=$NAMESPACE
```

---

## 2. Secrets

### 2a. Artifactory registry credentials (image push)

The `crane-push-corporate` task and the build task need a Docker `config.json`
to push/pull images to Artifactory.

```bash
# Create or download your Artifactory docker config
# Option A — docker login then copy the generated config
docker login artifacts.corp.com -u <user> -p <token>
cp ~/.docker/config.json /tmp/config.json

# Option B — write it by hand
cat > /tmp/config.json <<'EOF'
{
  "auths": {
    "artifacts.corp.com": {
      "auth": "<base64(user:token)>"
    }
  }
}
EOF

kubectl create secret generic artifactory-docker-config \
  --from-file=config.json=/tmp/config.json \
  -n $NAMESPACE
```

### 2b. Maven mirror credentials (Java builds)

Only needed when building Java projects.
Credentials are injected as env vars — **never stored in profile YAML**.

```bash
kubectl create secret generic artifactory-creds \
  --from-literal=MAVEN_MIRROR_USER=<your-ldap-user> \
  --from-literal=MAVEN_MIRROR_PASSWORD=<your-artifactory-token> \
  -n $NAMESPACE
```

### 2c. NuGet mirror credentials (.NET builds)

Only needed when building .NET projects.

```bash
kubectl create secret generic nuget-creds \
  --from-literal=NUGET_MIRROR_USER=<your-ldap-user> \
  --from-literal=NUGET_MIRROR_PASSWORD=<your-artifactory-token> \
  -n $NAMESPACE
```

> **Security note:** If you are not building Java or .NET projects you can skip 2b/2c.
> Both secrets are marked `optional: true` in the task — their absence does not fail the build.

### 2d. Corporate TLS CA certificate (if cluster uses TLS inspection)

```bash
kubectl create secret generic corporate-ca \
  --from-file=ca-bundle.crt=/path/to/corporate-ca.crt \
  -n $NAMESPACE
```

---

## 3. Persistent Volume Claims

Three PVCs cover all pipelines. Adjust `storage` to suit your project size.

```bash
kubectl apply -n $NAMESPACE -f - <<'EOF'
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: apexpack-source-pvc
spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 2Gi
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: apexpack-shared-pvc
spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 5Gi
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: apexpack-profiles-pvc
spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 256Mi
EOF
```

> `apexpack-profiles-pvc` is only required by `pipeline-full.yaml`.
> The simple `pipeline.yaml` and `pipeline-self.yaml` do not need it.

---

## 4. Apply Tasks and Pipelines

```bash
# apexpack detect/scan/patch Tasks (shared across all pipelines)
kubectl apply -n $NAMESPACE -f tekton/tasks/apexpack-detect.yaml
kubectl apply -n $NAMESPACE -f tekton/tasks/apexpack-scan.yaml
kubectl apply -n $NAMESPACE -f tekton/tasks/apexpack-patch.yaml

# Corporate-specific Tasks
kubectl apply -n $NAMESPACE -f tekton/corporate/tasks/

# Pipelines
kubectl apply -n $NAMESPACE -f tekton/corporate/pipeline.yaml
kubectl apply -n $NAMESPACE -f tekton/corporate/pipeline-full.yaml
kubectl apply -n $NAMESPACE -f tekton/corporate/pipeline-self.yaml
```

Verify everything landed:

```bash
tkn task list -n $NAMESPACE
tkn pipeline list -n $NAMESPACE
```

---

## 5. PipelineRun examples

### 5a. Simple pipeline (`apexpack-corporate`)

Build and push without CVE scanning. Good for a first-run smoke test.

```yaml
# pipelinerun-corporate-simple.yaml
apiVersion: tekton.dev/v1beta1
kind: PipelineRun
metadata:
  generateName: apexpack-corporate-simple-
  namespace: <your-namespace>
spec:
  pipelineRef:
    name: apexpack-corporate
  params:
    - name: app-id
      value: my-java-app
    - name: image-name
      value: artifacts.corp.com/my-repo/my-java-app
    - name: git-url
      value: https://github.com/my-org/my-java-app.git
    - name: git-revision
      value: main
    - name: release-strategy
      value: timestamped-hash
    - name: release-channels
      value: [latest]
    - name: apexpack-image
      value: artifacts.corp.com/apexpack/apexpack:latest
    # Optional: override runtime detection
    # - name: runtime
    #   value: java
  workspaces:
    - name: source
      persistentVolumeClaim:
        claimName: apexpack-source-pvc
    - name: shared
      persistentVolumeClaim:
        claimName: apexpack-shared-pvc
    - name: dockerconfig
      secret:
        secretName: artifactory-docker-config
    # Optional: TLS CA
    # - name: tls-ca
    #   secret:
    #     secretName: corporate-ca
```

```bash
kubectl create -f pipelinerun-corporate-simple.yaml -n $NAMESPACE
tkn pipelinerun logs --last -f -n $NAMESPACE
```

---

### 5b. Full pipeline (`apexpack-corporate-full`)

Build → scan → auto-patch CVEs → rebuild → rescan → push → tag.

```yaml
# pipelinerun-corporate-full.yaml
apiVersion: tekton.dev/v1beta1
kind: PipelineRun
metadata:
  generateName: apexpack-corporate-full-
  namespace: <your-namespace>
spec:
  pipelineRef:
    name: apexpack-corporate-full
  params:
    - name: app-id
      value: my-java-app
    - name: image-name
      value: artifacts.corp.com/my-repo/my-java-app
    - name: git-url
      value: https://github.com/my-org/my-java-app.git
    - name: git-revision
      value: main
    - name: release-strategy
      value: timestamped-hash
    - name: release-channels
      value: [latest]
    - name: apexpack-image
      value: artifacts.corp.com/apexpack/apexpack:latest
    - name: fail-on
      value: high          # fail scan on high or critical CVEs
    - name: auto-patch
      value: "true"        # override profile setting for this run
    - name: patch-persist
      value: ""            # use profile default (no git commit of patches)
  workspaces:
    - name: source
      persistentVolumeClaim:
        claimName: apexpack-source-pvc
    - name: shared
      persistentVolumeClaim:
        claimName: apexpack-shared-pvc
    - name: profiles
      persistentVolumeClaim:
        claimName: apexpack-profiles-pvc
    - name: dockerconfig
      secret:
        secretName: artifactory-docker-config
    # - name: tls-ca
    #   secret:
    #     secretName: corporate-ca
```

```bash
kubectl create -f pipelinerun-corporate-full.yaml -n $NAMESPACE
tkn pipelinerun logs --last -f -n $NAMESPACE
```

---

### 5c. Self-build pipeline (`apexpack-self-build`)

Builds and pushes the apexpack image itself. Requires an existing apexpack image
as the builder (bootstrapping — use the image from your first manual push).

```yaml
# pipelinerun-self-build.yaml
apiVersion: tekton.dev/v1beta1
kind: PipelineRun
metadata:
  generateName: apexpack-self-build-
  namespace: <your-namespace>
spec:
  pipelineRef:
    name: apexpack-self-build
  params:
    - name: app-id
      value: apexpack
    - name: image-name
      value: artifacts.corp.com/apexpack/apexpack
    - name: git-url
      value: https://github.com/my-org/apexpack.git
    - name: git-revision
      value: main
    - name: release-channels
      value: [latest]
    - name: go-image
      value: golang:1.23-alpine
    - name: apexpack-builder-image
      value: artifacts.corp.com/apexpack/apexpack:latest
  workspaces:
    - name: source
      persistentVolumeClaim:
        claimName: apexpack-source-pvc
    - name: shared
      persistentVolumeClaim:
        claimName: apexpack-shared-pvc
    - name: dockerconfig
      secret:
        secretName: artifactory-docker-config
```

```bash
kubectl create -f pipelinerun-self-build.yaml -n $NAMESPACE
tkn pipelinerun logs --last -f -n $NAMESPACE
```

---

## 6. Workspace mapping reference

| Pipeline workspace  | PVC / Secret                   | Required by             |
|---------------------|--------------------------------|-------------------------|
| `source`            | `apexpack-source-pvc`          | all pipelines           |
| `shared`            | `apexpack-shared-pvc`          | all pipelines           |
| `profiles`          | `apexpack-profiles-pvc`        | `pipeline-full` only    |
| `dockerconfig`          | `artifactory-docker-config` secret | all pipelines (optional) |
| `tls-ca`            | `corporate-ca` secret          | optional — TLS proxy only |

---

## 7. Checking results

```bash
# List all PipelineRuns
tkn pipelinerun list -n $NAMESPACE

# Tail logs of the most recent run
tkn pipelinerun logs --last -f -n $NAMESPACE

# Inspect a specific TaskRun
tkn taskrun describe <taskrun-name> -n $NAMESPACE

# Get the pushed image reference from a run
tkn pipelinerun describe --last -n $NAMESPACE \
  -o jsonpath='{.status.pipelineResults[?(@.name=="images.pushed-image")].value}'
```

---

## 8. Cleanup between test runs

The PVCs persist between runs. If you want a clean state:

```bash
# Delete and recreate source/shared PVCs to flush stale build artifacts
kubectl delete pvc apexpack-source-pvc apexpack-shared-pvc -n $NAMESPACE
kubectl apply -n $NAMESPACE -f - <<'EOF'
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: apexpack-source-pvc
spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 2Gi
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: apexpack-shared-pvc
spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 5Gi
EOF
```

> Leave `apexpack-profiles-pvc` intact between runs — `pipeline-full` seeds it from the
> apexpack image on first use and updates it in place when patches are applied.

---

## 9. Troubleshooting

| Symptom | Likely cause | Fix |
|---------|-------------|-----|
| `crane push` fails with 401 | Docker config not mounted or wrong registry hostname | Check `artifactory-docker-config` secret contains the correct registry |
| `apexpack-build-corporate` step exits with `tls: failed to verify certificate` | TLS-intercepting proxy | Bind `tls-ca` workspace to the `corporate-ca` secret |
| `nothing provides aspnet-8-runtime` | Wrong apexpack image version | Update `apexpack-image` param to a version that includes the correct Wolfi packages |
| `unsupported dotnet version "6"` | .NET 6 is EOL and not in Wolfi | Upgrade project to .NET 8 or 9 |
| Patch task skipped even though scan failed | `AUTO_PATCH` result from detect is `false` | Set `auto-patch: "true"` param on the PipelineRun or enable in your profile YAML |
| Profiles PVC empty / detect task fails | First run on a fresh PVC | The detect task seeds profiles automatically — if it fails, check the apexpack image can be pulled |
