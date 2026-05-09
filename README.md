# mock-dcgm-exporter-mig

模擬 NVIDIA dcgm-exporter 在 MIG 模式下的 Prometheus metrics 輸出，用於在沒有實體 MIG GPU 的環境下開發和測試監控功能。

## 設定檔

透過 `config.yaml` 定義 GPU 拓撲和 MIG 切割方式，不需要修改程式碼。

預設設定（`config.yaml`）：

```yaml
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

| 欄位 | 說明 | 對應 dcgm-exporter label |
|---|---|---|
| `model` | GPU 型號 | `modelName` |
| `uuid` | GPU UUID | `UUID` |
| `memory_gb` | 整張 GPU 總記憶體 | — |
| `driver_version` | 驅動版本 | `DCGM_FI_DRIVER_VERSION` |
| `hostname` | 節點名稱 | `Hostname` |
| `profile` | MIG profile | `GPU_I_PROFILE` |
| `gi_id` | GPU Instance ID | `GPU_I_ID` |
| `namespace` | K8s namespace | `namespace` |
| `pod` | K8s pod 名稱 | `pod` |
| `container` | K8s container 名稱 | `container` |

每個 instance 的記憶體大小從 `profile` 自動解析（如 `3g.40gb` → 40GB = 40960 MiB）。

指定設定檔路徑：

```bash
# 命令列參數
./mock-dcgm-exporter-mig -config /path/to/config.yaml

# 環境變數
CONFIG_PATH=/path/to/config.yaml ./mock-dcgm-exporter-mig

# Docker
docker run -v ./my-config.yaml:/config.yaml:ro mock-dcgm-exporter-mig:latest
```

## 模擬規格

預設設定模擬同一節點上 2 張 NVIDIA H100 80GB HBM3，各自不同 MIG 切法：

**GPU 0** — `3g.40gb × 2 + 1g.10gb`

| GI ID | Profile | 記憶體 | Namespace | Pod |
|---|---|---|---|---|
| 1 | `3g.40gb` | 40 GB | project-ml-train | train-job-a-* |
| 2 | `3g.40gb` | 40 GB | project-ml-train | train-job-b-* |
| 7 | `1g.10gb` | 10 GB | project-inference | infer-svc-d-* |

**GPU 1** — `4g.40gb + 2g.20gb + 1g.10gb`

| GI ID | Profile | 記憶體 | Namespace | Pod |
|---|---|---|---|---|
| 0 | `4g.40gb` | 40 GB | project-ml-train | train-job-c-* |
| 1 | `2g.20gb` | 20 GB | project-data | preprocess-a-* |
| 7 | `1g.10gb` | 10 GB | project-inference | infer-svc-e-* |

### 輸出指標

#### Per-GI 獨立指標（每個 MIG instance 各自獨立變動）

| 指標 | 說明 |
|---|---|
| `DCGM_FI_PROF_GR_ENGINE_ACTIVE` | GPU 使用率（0.0-1.0），MIG 下取代 `GPU_UTIL` 的推薦指標 |
| `DCGM_FI_PROF_SM_ACTIVE` | SM 活躍比率 |
| `DCGM_FI_PROF_PIPE_TENSOR_ACTIVE` | Tensor pipe 活躍比率 |
| `DCGM_FI_PROF_DRAM_ACTIVE` | DRAM 活躍比率 |
| `DCGM_FI_DEV_FB_USED` | 顯存已用 (MiB) |
| `DCGM_FI_DEV_FB_FREE` | 顯存剩餘 (MiB) |
| `DCGM_FI_DEV_FB_RESERVED` | 保留顯存 (MiB) |

#### 物理共享指標（同一 GPU 上所有 GI 數值相近）

| 指標 | 說明 |
|---|---|
| `DCGM_FI_DEV_GPU_TEMP` | GPU 溫度 (C) |
| `DCGM_FI_DEV_POWER_USAGE` | 耗電 (W) |
| `DCGM_FI_DEV_SM_CLOCK` | SM 時脈 (MHz) |
| `DCGM_FI_DEV_MEM_CLOCK` | 記憶體時脈 (MHz) |
| `DCGM_FI_DEV_XID_ERRORS` | XID 錯誤 |

### MIG Label 格式

每筆指標帶有以下 label，與真實 dcgm-exporter MIG 輸出一致：

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

### 數值變動方式 — Ornstein-Uhlenbeck Process

使用 [Ornstein-Uhlenbeck 過程](https://en.wikipedia.org/wiki/Ornstein%E2%80%93Uhlenbeck_process)（均值回歸隨機過程）產生自然波動：

```
dX = θ(μ - X)dt + σdW
```

| 符號 | 意義 | 效果 |
|---|---|---|
| `X` | 目前值 | 指標的當前數值 |
| `μ` | 長期均值 | 基準值（如 GPU 使用率 60%） |
| `θ` | 均值回歸速度 | 越大→越快拉回 μ；越小→越自由漂移 |
| `σ` | 波動幅度 | 越大→波動越劇烈 |
| `dW` | 維納過程增量 | 隨機項 N(0, √dt) |

程式每秒執行一次離散化步進（Euler-Maruyama）：

```
X(t+Δt) = X(t) + θ·(μ - X(t))·Δt + σ·√Δt·N(0,1)
```

**特性**：離 μ 越遠拉回力越強，數值不會漂移到離譜範圍。每次啟動路徑不同，Grafana 上看起來像真實負載波動。

### 與官方 dcp-metrics-included.csv 的覆蓋對照

[dcp-metrics-included.csv](https://github.com/NVIDIA/dcgm-exporter/blob/main/etc/dcp-metrics-included.csv) 是 dcgm-exporter 官方內建的指標定義檔，定義了 Prometheus 要收集哪些 DCGM field。以下是官方檔案的每一項指標及本 mock server 的覆蓋狀況：

| 官方 CSV 指標 | 類型 | 說明 | Mock 覆蓋 | 備註 |
|---|---|---|---|---|
| `DCGM_FI_DEV_SM_CLOCK` | gauge | SM 時脈 (MHz) | 有 | 物理共享，OU 模擬 |
| `DCGM_FI_DEV_MEM_CLOCK` | gauge | 記憶體時脈 (MHz) | 有 | 固定值 1593 |
| `DCGM_FI_DEV_MEMORY_TEMP` | gauge | 記憶體溫度 (C) | 有 | 物理共享，OU 模擬 |
| `DCGM_FI_DEV_GPU_TEMP` | gauge | GPU 溫度 (C) | 有 | 物理共享，OU 模擬 |
| `DCGM_FI_DEV_POWER_USAGE` | gauge | 耗電 (W) | 有 | 物理共享，OU 模擬 |
| `DCGM_FI_DEV_TOTAL_ENERGY_CONSUMPTION` | counter | 總能耗 (mJ) | 有 | 由 POWER_USAGE 累積 |
| `DCGM_FI_DEV_PCIE_REPLAY_COUNTER` | counter | PCIe 重試次數 | 有 | 固定值 0 |
| `DCGM_FI_DEV_GPU_UTIL` | gauge | GPU 使用率 (%) | **刻意不模擬** | MIG 下不可用，靜默跳過 |
| `DCGM_FI_DEV_MEM_COPY_UTIL` | gauge | 記憶體使用率 (%) | **刻意不模擬** | MIG 下不可用 |
| `DCGM_FI_DEV_ENC_UTIL` | gauge | 編碼器使用率 (%) | **刻意不模擬** | MIG 下不可用 |
| `DCGM_FI_DEV_DEC_UTIL` | gauge | 解碼器使用率 (%) | **刻意不模擬** | MIG 下不可用 |
| `DCGM_FI_DEV_XID_ERRORS` | gauge | 最後一次 XID 錯誤 | 有 | 固定值 0 |
| `DCGM_FI_DEV_FB_FREE` | gauge | 顯存剩餘 (MiB) | 有 | Per-GI，由 FB_USED 反算 |
| `DCGM_FI_DEV_FB_USED` | gauge | 顯存已用 (MiB) | 有 | Per-GI，OU 模擬 |
| `DCGM_FI_DEV_FB_RESERVED` | gauge | 保留顯存 (MiB) | 有 | 固定值 (FBTotal * 2%) |
| `DCGM_FI_DEV_NVLINK_BANDWIDTH_TOTAL` | counter | NVLink 頻寬 | 有 | 由 PCIe 流量累積 |
| `DCGM_FI_DEV_VGPU_LICENSE_STATUS` | gauge | vGPU 授權狀態 | 有 | 固定值 1 (licensed) |
| `DCGM_FI_DEV_UNCORRECTABLE_REMAPPED_ROWS` | counter | 不可修正重映射列數 | 有 | 固定值 0 |
| `DCGM_FI_DEV_CORRECTABLE_REMAPPED_ROWS` | counter | 可修正重映射列數 | 有 | 固定值 0 |
| `DCGM_FI_DEV_ROW_REMAP_FAILURE` | gauge | 列重映射是否失敗 | 有 | 固定值 0 |
| `DCGM_FI_DRIVER_VERSION` | label | 驅動版本 | 有 | 以 label 形式附加 |
| `DCGM_FI_PROF_GR_ENGINE_ACTIVE` | gauge | 圖形引擎活躍比率 | 有 | **MIG 核心指標**，OU 模擬 |
| `DCGM_FI_PROF_PIPE_TENSOR_ACTIVE` | gauge | Tensor pipe 活躍比率 | 有 | Per-GI，OU 模擬 |
| `DCGM_FI_PROF_DRAM_ACTIVE` | gauge | DRAM 活躍比率 | 有 | Per-GI，OU 模擬 |
| `DCGM_FI_PROF_PCIE_TX_BYTES` | gauge | PCIe 發送速率 (B/s) | 有 | 物理共享，OU 模擬 |
| `DCGM_FI_PROF_PCIE_RX_BYTES` | gauge | PCIe 接收速率 (B/s) | 有 | 物理共享，OU 模擬 |

**覆蓋統計**：官方 CSV 共 25 項指標，其中 4 項 MIG 下不可用（`GPU_UTIL`、`MEM_COPY_UTIL`、`ENC_UTIL`、`DEC_UTIL`，真實 dcgm-exporter 也會靜默跳過）刻意不模擬。剩餘 21 項全部覆蓋。另外額外加入 1 項 `SM_ACTIVE`（官方 CSV 未列但 MIG 下可用的 profiling 指標），共輸出 22 項。

## 安裝

### Helm

```bash
# 從本地 chart 安裝
helm install mock-dcgm charts/mock-dcgm-exporter-mig

# 自訂 MIG 設定
helm install mock-dcgm charts/mock-dcgm-exporter-mig \
  --set-file config=my-config.yaml

# 啟用 ServiceMonitor（需要 Prometheus Operator）
helm install mock-dcgm charts/mock-dcgm-exporter-mig \
  --set serviceMonitor.enabled=true

# 指定 image
helm install mock-dcgm charts/mock-dcgm-exporter-mig \
  --set image.repository=vulcanshen2304/mock-dcgm-exporter-mig \
  --set image.tag=v0.1.0
```

### Kustomize

```bash
# 直接部署
kubectl apply -k deploy/kustomize/base

# 修改 image
cd deploy/kustomize/base
kustomize edit set image mock-dcgm-exporter-mig=vulcanshen2304/mock-dcgm-exporter-mig:v0.1.0
kubectl apply -k .
```

自訂 MIG 設定：編輯 `deploy/kustomize/base/configmap.yaml` 中的 `config.yaml` 內容。

### Docker

```bash
docker run -d --name mock-dcgm -p 9400:9400 vulcanshen2304/mock-dcgm-exporter-mig:latest

# 掛載自訂設定
docker run -d --name mock-dcgm -p 9400:9400 \
  -v ./config.yaml:/config.yaml:ro \
  vulcanshen2304/mock-dcgm-exporter-mig:latest
```

## 開發

### 直接執行

```bash
go build -o mock-dcgm-exporter-mig .
./mock-dcgm-exporter-mig                         # 使用 ./config.yaml
./mock-dcgm-exporter-mig -config my-config.yaml  # 指定設定檔
# metrics endpoint: http://localhost:9400/metrics
```

### Docker Bake（建置 tar 檔）

預設輸出 amd64 和 arm64 兩個平台的 tar 檔到 `dist/`：

```bash
# 建置兩個平台的 tar
docker buildx bake

# 產出：
#   dist/mock-dcgm-exporter-mig-amd64.tar
#   dist/mock-dcgm-exporter-mig-arm64.tar

# 指定 tag
TAG=v1.0.0 docker buildx bake
```

### 部署到測試機

```bash
# 將 tar 複製到測試機
scp dist/mock-dcgm-exporter-mig-amd64.tar user@target-host:~/

# 在測試機上載入並啟動
ssh user@target-host
docker load -i mock-dcgm-exporter-mig-amd64.tar
docker run -d --name mock-dcgm-exporter-mig -p 9400:9400 mock-dcgm-exporter-mig:latest
```

## 本地開發（Docker Compose）

專案內附 `compose.yaml` + `prometheus.yml`，一行指令啟動 mock exporter + Prometheus：

```bash
docker compose up -d
```

| 服務 | URL | 說明 |
|---|---|---|
| mock-dcgm-exporter-mig | http://localhost:9400/metrics | 模擬的 MIG 指標 endpoint |
| Prometheus | http://localhost:9090 | Prometheus UI，可查詢和看 Graph |

停止：

```bash
docker compose down
```

### 檔案說明

**`compose.yaml`** — 定義兩個服務：

```yaml
services:
  mock-dcgm-exporter-mig:     # 從 Dockerfile 建置，expose port 9400
    build: .
    ports:
      - "9400:9400"
    volumes:
      - ./config.yaml:/config.yaml:ro   # 掛載設定檔，修改後重啟即生效

  prometheus:              # 官方 Prometheus image，掛載本地 config
    image: prom/prometheus:latest
    ports:
      - "9090:9090"
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml:ro
    depends_on:
      - mock-dcgm-exporter-mig
```

**`prometheus.yml`** — Prometheus 抓取設定：

```yaml
global:
  scrape_interval: 1s      # 每秒抓取一次（開發用，生產建議 15s）

scrape_configs:
  - job_name: 'dcgm-exporter'
    static_configs:
      - targets: ['mock-dcgm-exporter-mig:9400']   # compose 內部 DNS
```

## Prometheus 設定（獨立部署）

如果 Prometheus 是獨立部署的（非 compose），在 `prometheus.yml` 加入：

```yaml
scrape_configs:
  - job_name: 'dcgm-exporter'
    scrape_interval: 15s
    static_configs:
      - targets: ['<mock-server-ip>:9400']
```

如果部署在 Kubernetes 中，Helm chart 已內建 ServiceMonitor 支援（`--set serviceMonitor.enabled=true`）。

### PromQL 查詢範例

```promql
# 顯存使用百分比 (per-MIG-instance)
100 * DCGM_FI_DEV_FB_USED{pod!=""} / (DCGM_FI_DEV_FB_USED{pod!=""} + DCGM_FI_DEV_FB_FREE{pod!=""})

# GPU 使用率 (per-MIG-instance)
DCGM_FI_PROF_GR_ENGINE_ACTIVE{pod!=""}

# 耗電 (同卡共享，依 GPU UUID 分組)
DCGM_FI_DEV_POWER_USAGE{GPU_I_ID="1"}

# 溫度
DCGM_FI_DEV_GPU_TEMP{GPU_I_ID="1"}
```

## 自訂模擬配置

修改 `config.yaml` 即可自訂 GPU 拓撲，支援多卡、不同 MIG 切法。

多卡範例：

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

## 專案結構

```
mock-dcgm-exporter-mig/
├── main.go                 # 主程式
├── config.yaml             # 預設 MIG 設定
├── go.mod / go.sum
├── Dockerfile              # 本地開發用（multi-stage build）
├── Dockerfile.release      # GoReleaser 用（從編譯好的 binary 打包）
├── .goreleaser.yaml        # GoReleaser 設定
├── docker-bake.hcl         # 本地 multi-arch tar 建置
├── compose.yaml            # 本地開發（exporter + Prometheus）
├── prometheus.yml          # Prometheus 抓取設定
├── charts/                 # Helm chart
│   └── mock-dcgm-exporter-mig/
│       ├── Chart.yaml
│       ├── values.yaml
│       └── templates/
└── deploy/                 # Kustomize
    └── kustomize/
        └── base/
```

## 參考文件

- [NVIDIA MIG User Guide](https://docs.nvidia.com/datacenter/tesla/mig-user-guide/latest/)
- [NVIDIA MIG Supported GPUs](https://docs.nvidia.com/datacenter/tesla/mig-user-guide/supported-gpus.html)
- [NVIDIA dcgm-exporter GitHub](https://github.com/NVIDIA/dcgm-exporter)
- [NVIDIA DCGM Exporter Documentation](https://docs.nvidia.com/datacenter/cloud-native/gpu-telemetry/latest/dcgm-exporter.html)
- [DCGM API Field IDs](https://docs.nvidia.com/datacenter/dcgm/latest/dcgm-api/dcgm-api-field-ids.html)
- [dcp-metrics-included.csv (官方指標檔)](https://github.com/NVIDIA/dcgm-exporter/blob/main/etc/dcp-metrics-included.csv)
