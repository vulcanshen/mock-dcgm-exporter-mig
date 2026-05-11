# mock-dcgm-exporter-mig

Mock NVIDIA dcgm-exporter that simulates MIG (Multi-Instance GPU) Prometheus metrics output, designed for developing and testing GPU monitoring features without physical MIG hardware.

## Installation

### Helm

```bash
helm install mock-dcgm \
  https://github.com/vulcanshen/mock-dcgm-exporter-mig/releases/download/v0.3.0/mock-dcgm-exporter-mig-0.3.0.tgz

# Custom MIG config
helm install mock-dcgm \
  https://github.com/vulcanshen/mock-dcgm-exporter-mig/releases/download/v0.3.0/mock-dcgm-exporter-mig-0.3.0.tgz \
  --set-file config=my-config.yaml

# Enable ServiceMonitor (requires Prometheus Operator)
helm install mock-dcgm \
  https://github.com/vulcanshen/mock-dcgm-exporter-mig/releases/download/v0.3.0/mock-dcgm-exporter-mig-0.3.0.tgz \
  --set serviceMonitor.enabled=true

# Specify image
helm install mock-dcgm \
  https://github.com/vulcanshen/mock-dcgm-exporter-mig/releases/download/v0.3.0/mock-dcgm-exporter-mig-0.3.0.tgz \
  --set image.repository=vulcanshen2304/mock-dcgm-exporter-mig \
  --set image.tag=0.3.0
```

### Kustomize

```bash
# Deploy directly from GitHub
kubectl apply -k https://github.com/vulcanshen/mock-dcgm-exporter-mig/deploy/kustomize/base

# Or clone and customize image
git clone https://github.com/vulcanshen/mock-dcgm-exporter-mig.git
cd mock-dcgm-exporter-mig/deploy/kustomize/base
kustomize edit set image mock-dcgm-exporter-mig=vulcanshen2304/mock-dcgm-exporter-mig:0.3.0
kubectl apply -k .
```

### Docker

```bash
docker run -d --name mock-dcgm -p 9400:9400 vulcanshen2304/mock-dcgm-exporter-mig:latest

# With custom config
docker run -d --name mock-dcgm -p 9400:9400 \
  -v ./config.yaml:/config.yaml:ro \
  vulcanshen2304/mock-dcgm-exporter-mig:latest
```

## Configuration

GPU topology and MIG partitioning are defined in `config.yaml`. No code changes needed.

### CSV profile

Controls which set of metrics to output, matching real dcgm-exporter CSV configurations:

```yaml
# "dcp-metrics-included" (default) — 22 metrics, includes PROF_* profiling metrics
# "default-counters"               — 12 metrics, matches older/default deployments
csv_profile: "dcp-metrics-included"
```

| Profile | Metrics | PROF_GR_ENGINE_ACTIVE | Use case |
|---|---|---|---|
| `dcp-metrics-included` | 22 | Yes | Proper MIG monitoring setup |
| `default-counters` | 12 | No | Simulating environments without DCP config |

### GPU model presets

MEM_CLOCK, idle/max power, and MIG slice count are auto-derived from the `model` field:

| Model | MEM_CLOCK | Idle Power | Max Power | MIG Slices |
|---|---|---|---|---|
| H200 | 2619 MHz | 130W | 700W | 7 |
| H100 | 1593 MHz | 120W | 700W | 7 |
| A100 80GB | 1215 MHz | 45W | 400W | 7 |
| A100 (40GB) | 1215 MHz | 35W | 300W | 7 |
| A30 | 1215 MHz | 25W | 165W | 4 |

### GPU topology

Default config:

```yaml
csv_profile: "dcp-metrics-included"

gpus:
  - model: "NVIDIA H100 80GB HBM3"
    uuid: "GPU-559c7f49-e3ff-0358-0c74-8f7eeb60b7b0"
    memory_gb: 80
    driver_version: "570.195.03"
    hostname: "mig-dev-node"
    instances:
      - profile: "3g.40gb"
        gi_id: 1
        namespace: "project-ml-train"
        pod: "train-job-a-7f8d4b-x9k2p"
        container: "tf"
      - profile: "3g.40gb"
        gi_id: 2
        namespace: "project-ml-train"
        pod: "train-job-b-3c5a1e-m4n7q"
        container: "tf"
      - profile: "1g.10gb"
        gi_id: 7
        namespace: "project-inference"
        pod: "infer-svc-d9e2f1-j3h8w"
        container: "serving"
```

| Field | Description | dcgm-exporter label |
|---|---|---|
| `model` | GPU model name | `modelName` |
| `uuid` | GPU UUID | `UUID` |
| `memory_gb` | Total GPU memory | — |
| `driver_version` | Driver version | `DCGM_FI_DRIVER_VERSION` |
| `hostname` | Node hostname | `Hostname` |
| `profile` | MIG profile | `GPU_I_PROFILE` |
| `gi_id` | GPU Instance ID | `GPU_I_ID` |
| `namespace` | K8s namespace | `namespace` |
| `pod` | K8s pod name | `pod` |
| `container` | K8s container name | `container` |

Instance memory is automatically parsed from the profile (e.g. `3g.40gb` = 40GB = 40960 MiB).

Specifying config path:

```bash
# CLI flag
./mock-dcgm-exporter-mig -config /path/to/config.yaml

# Environment variable
CONFIG_PATH=/path/to/config.yaml ./mock-dcgm-exporter-mig

# Docker
docker run -v ./my-config.yaml:/config.yaml:ro vulcanshen2304/mock-dcgm-exporter-mig:latest
```

### Multi-GPU example

```yaml
gpus:
  - model: "NVIDIA A100 80GB"
    uuid: "GPU-aaaa-bbbb-cccc"
    memory_gb: 80
    driver_version: "570.195.03"
    hostname: "node-1"
    instances:
      - profile: "7g.80gb"
        gi_id: 0
        namespace: "ml"
        pod: "full-gpu-job-xxx"
        container: "train"
  - model: "NVIDIA A100 40GB"
    uuid: "GPU-dddd-eeee-ffff"
    memory_gb: 40
    driver_version: "570.195.03"
    hostname: "node-1"
    instances:
      - profile: "1g.5gb"
        gi_id: 1
        namespace: "inference"
        pod: "infer-a-xxx"
        container: "serve"
      - profile: "1g.5gb"
        gi_id: 2
        namespace: "inference"
        pod: "infer-b-xxx"
        container: "serve"
```

## Default Simulation

Simulates 2 NVIDIA H100 80GB HBM3 GPUs on the same node with different MIG partitioning:

**GPU 0** — `3g.40gb x 2 + 1g.10gb`

| GI ID | Profile | Memory | Namespace | Pod |
|---|---|---|---|---|
| 1 | `3g.40gb` | 40 GB | project-ml-train | train-job-a-* |
| 2 | `3g.40gb` | 40 GB | project-ml-train | train-job-b-* |
| 7 | `1g.10gb` | 10 GB | project-inference | infer-svc-d-* |

**GPU 1** — `4g.40gb + 2g.20gb + 1g.10gb`

| GI ID | Profile | Memory | Namespace | Pod |
|---|---|---|---|---|
| 0 | `4g.40gb` | 40 GB | project-ml-train | train-job-c-* |
| 1 | `2g.20gb` | 20 GB | project-data | preprocess-a-* |
| 7 | `1g.10gb` | 10 GB | project-inference | infer-svc-e-* |

## Metrics

### Per-GI metrics (independent per MIG instance)

| Metric | Description |
|---|---|
| `DCGM_FI_PROF_GR_ENGINE_ACTIVE` | GPU utilization, recommended metric for MIG (replaces `GPU_UTIL`). Max value scales with MIG slice ratio — e.g. `3g` on H100 (7 slices) → max ~0.43 ([DCGM#138](https://github.com/NVIDIA/DCGM/issues/138)) |
| `DCGM_FI_PROF_SM_ACTIVE` | SM active ratio (same scaling as GR_ENGINE_ACTIVE) |
| `DCGM_FI_PROF_PIPE_TENSOR_ACTIVE` | Tensor pipe active ratio |
| `DCGM_FI_PROF_DRAM_ACTIVE` | DRAM active ratio |
| `DCGM_FI_DEV_FB_USED` | Framebuffer used (MiB) |
| `DCGM_FI_DEV_FB_FREE` | Framebuffer free (MiB) |
| `DCGM_FI_DEV_FB_RESERVED` | Framebuffer reserved (MiB) |

### Shared metrics (same value for all GIs on same physical GPU)

| Metric | Description |
|---|---|
| `DCGM_FI_DEV_GPU_TEMP` | GPU temperature (C) |
| `DCGM_FI_DEV_POWER_USAGE` | Power draw (W) |
| `DCGM_FI_DEV_SM_CLOCK` | SM clock (MHz) |
| `DCGM_FI_DEV_MEM_CLOCK` | Memory clock (MHz) |
| `DCGM_FI_DEV_XID_ERRORS` | XID errors |

### MIG label format

Each metric includes labels matching real dcgm-exporter MIG output:

```
DCGM_FI_DEV_FB_USED{
  gpu="0",
  UUID="GPU-559c7f49-...",
  device="nvidia0",
  modelName="NVIDIA H100 80GB HBM3",
  GPU_I_PROFILE="3g.40gb",
  GPU_I_ID="1",
  Hostname="mig-dev-node",
  DCGM_FI_DRIVER_VERSION="570.195.03",
  namespace="project-ml-train",
  pod="train-job-a-7f8d4b-x9k2p",
  container="tf"
} 18432
```

### Coverage against official dcp-metrics-included.csv

[dcp-metrics-included.csv](https://github.com/NVIDIA/dcgm-exporter/blob/main/etc/dcp-metrics-included.csv) is the official metric definition file shipped with dcgm-exporter. Coverage status:

| Official CSV metric | Type | Mock | Note |
|---|---|---|---|
| `DCGM_FI_DEV_SM_CLOCK` | gauge | Yes | Shared, simulated |
| `DCGM_FI_DEV_MEM_CLOCK` | gauge | Yes | Fixed 1593 |
| `DCGM_FI_DEV_MEMORY_TEMP` | gauge | Yes | Shared, simulated |
| `DCGM_FI_DEV_GPU_TEMP` | gauge | Yes | Shared, simulated |
| `DCGM_FI_DEV_POWER_USAGE` | gauge | Yes | Shared, simulated |
| `DCGM_FI_DEV_TOTAL_ENERGY_CONSUMPTION` | counter | Yes | Accumulated from POWER_USAGE |
| `DCGM_FI_DEV_PCIE_REPLAY_COUNTER` | counter | Yes | Fixed 0 |
| `DCGM_FI_DEV_GPU_UTIL` | gauge | **Skipped** | Not available in MIG mode |
| `DCGM_FI_DEV_MEM_COPY_UTIL` | gauge | **Skipped** | Not available in MIG mode |
| `DCGM_FI_DEV_ENC_UTIL` | gauge | **Skipped** | Not available in MIG mode |
| `DCGM_FI_DEV_DEC_UTIL` | gauge | **Skipped** | Not available in MIG mode |
| `DCGM_FI_DEV_XID_ERRORS` | gauge | Yes | Fixed 0 |
| `DCGM_FI_DEV_FB_FREE` | gauge | Yes | Per-GI, derived from FB_USED |
| `DCGM_FI_DEV_FB_USED` | gauge | Yes | Per-GI, simulated |
| `DCGM_FI_DEV_FB_RESERVED` | gauge | Yes | Fixed (FBTotal * 2%) |
| `DCGM_FI_DEV_NVLINK_BANDWIDTH_TOTAL` | counter | Yes | Accumulated from PCIe traffic |
| `DCGM_FI_DEV_VGPU_LICENSE_STATUS` | gauge | Yes | Fixed 1 |
| `DCGM_FI_DEV_UNCORRECTABLE_REMAPPED_ROWS` | counter | Yes | Fixed 0 |
| `DCGM_FI_DEV_CORRECTABLE_REMAPPED_ROWS` | counter | Yes | Fixed 0 |
| `DCGM_FI_DEV_ROW_REMAP_FAILURE` | gauge | Yes | Fixed 0 |
| `DCGM_FI_DRIVER_VERSION` | label | Yes | Attached as label |
| `DCGM_FI_PROF_GR_ENGINE_ACTIVE` | gauge | Yes | **Core MIG metric**, simulated |
| `DCGM_FI_PROF_PIPE_TENSOR_ACTIVE` | gauge | Yes | Per-GI, simulated |
| `DCGM_FI_PROF_DRAM_ACTIVE` | gauge | Yes | Per-GI, simulated |
| `DCGM_FI_PROF_PCIE_TX_BYTES` | gauge | Yes | Shared, simulated |
| `DCGM_FI_PROF_PCIE_RX_BYTES` | gauge | Yes | Shared, simulated |

**Coverage**: Official CSV contains 26 entries (25 metrics + 1 label). 4 metrics (`GPU_UTIL`, `MEM_COPY_UTIL`, `ENC_UTIL`, `DEC_UTIL`) are intentionally skipped as they are not available in MIG mode (real dcgm-exporter silently skips them too). Remaining 21 metrics all covered. 1 additional metric `SM_ACTIVE` (commented out in official CSV but available in MIG mode) is also included, totaling 22 metrics output.

Label format verified against real dcgm-exporter MIG output ([issue #512](https://github.com/NVIDIA/dcgm-exporter/issues/512), [issue #544](https://github.com/NVIDIA/dcgm-exporter/issues/544)) — label names, casing, and order match real hardware output.

### Metric simulation — Ornstein-Uhlenbeck Process

Metrics fluctuate using the [Ornstein-Uhlenbeck process](https://en.wikipedia.org/wiki/Ornstein%E2%80%93Uhlenbeck_process) (mean-reverting stochastic process):

```
dX = theta(mu - X)dt + sigma * dW
```

| Symbol | Meaning | Effect |
|---|---|---|
| `X` | Current value | Current metric reading |
| `mu` | Long-term mean | Baseline (e.g. GPU utilization 60%) |
| `theta` | Mean-reversion speed | Higher = snaps back to mu faster |
| `sigma` | Volatility | Higher = larger fluctuations |
| `dW` | Wiener process increment | Random term N(0, sqrt(dt)) |

Discrete step (Euler-Maruyama), executed every second:

```
X(t+dt) = X(t) + theta * (mu - X(t)) * dt + sigma * sqrt(dt) * N(0,1)
```

The farther X drifts from mu, the stronger the pull back. Each restart produces a different path. On Grafana, it looks like real workload fluctuation.

## Development

### Build and run

```bash
go build -o mock-dcgm-exporter-mig .
./mock-dcgm-exporter-mig                         # uses ./config.yaml
./mock-dcgm-exporter-mig -config my-config.yaml  # specify config
# metrics endpoint: http://localhost:9400/metrics
```

### Docker Compose (local dev)

Includes `compose.yaml` + `prometheus.yml` for one-command local setup:

```bash
docker compose up -d
```

| Service | URL | Description |
|---|---|---|
| mock-dcgm-exporter-mig | http://localhost:9400/metrics | Mock MIG metrics endpoint |
| Prometheus | http://localhost:9090 | Prometheus UI for queries and graphs |

```bash
docker compose down
```

### Docker Bake (build tar)

Build multi-arch tar files to `dist/`:

```bash
docker buildx bake

# Output:
#   dist/mock-dcgm-exporter-mig-amd64.tar
#   dist/mock-dcgm-exporter-mig-arm64.tar
```

### Prometheus config (standalone)

If Prometheus is deployed separately, add to `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: 'dcgm-exporter'
    scrape_interval: 15s
    static_configs:
      - targets: ['<mock-server-ip>:9400']
```

For Kubernetes, the Helm chart includes built-in ServiceMonitor support (`--set serviceMonitor.enabled=true`).

### PromQL examples

```promql
# Framebuffer usage percentage (per MIG instance)
100 * DCGM_FI_DEV_FB_USED{pod!=""} / (DCGM_FI_DEV_FB_USED{pod!=""} + DCGM_FI_DEV_FB_FREE{pod!=""})

# GPU utilization (per MIG instance)
DCGM_FI_PROF_GR_ENGINE_ACTIVE{pod!=""}

# Power draw (shared per physical GPU)
DCGM_FI_DEV_POWER_USAGE{GPU_I_ID="1"}

# Temperature
DCGM_FI_DEV_GPU_TEMP{GPU_I_ID="1"}
```

## Project structure

```
mock-dcgm-exporter-mig/
├── main.go                 # Main application
├── config.yaml             # Default MIG topology config
├── go.mod / go.sum
├── Dockerfile              # Local dev (multi-stage build)
├── Dockerfile.release      # GoReleaser (copies pre-built binary)
├── .goreleaser.yaml        # GoReleaser config
├── docker-bake.hcl         # Local multi-arch tar build
├── compose.yaml            # Local dev (exporter + Prometheus)
├── prometheus.yml          # Prometheus scrape config
├── charts/                 # Helm chart
│   └── mock-dcgm-exporter-mig/
│       ├── Chart.yaml
│       ├── values.yaml
│       └── templates/
└── deploy/                 # Kustomize
    └── kustomize/
        └── base/
```

## References

- [NVIDIA MIG User Guide](https://docs.nvidia.com/datacenter/tesla/mig-user-guide/latest/)
- [NVIDIA MIG Supported GPUs](https://docs.nvidia.com/datacenter/tesla/mig-user-guide/supported-gpus.html)
- [NVIDIA dcgm-exporter GitHub](https://github.com/NVIDIA/dcgm-exporter)
- [NVIDIA DCGM Exporter Documentation](https://docs.nvidia.com/datacenter/cloud-native/gpu-telemetry/latest/dcgm-exporter.html)
- [DCGM API Field IDs](https://docs.nvidia.com/datacenter/dcgm/latest/dcgm-api/dcgm-api-field-ids.html)
- [dcp-metrics-included.csv (official metrics file)](https://github.com/NVIDIA/dcgm-exporter/blob/main/etc/dcp-metrics-included.csv)

## License

[MIT](LICENSE)
