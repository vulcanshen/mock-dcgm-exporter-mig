package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Ornstein-Uhlenbeck Process
//
//   dX = θ(μ - X)dt + σdW
//
//   θ (theta) : mean-reversion speed
//   μ (mu)    : long-term mean
//   σ (sigma) : volatility
//   dW        : Wiener process increment — N(0, √dt)
//
// Discrete (Euler-Maruyama):
//   X(t+Δt) = X(t) + θ·(μ - X(t))·Δt + σ·√Δt·N(0,1)
// ---------------------------------------------------------------------------

type OUProcess struct {
	mu    float64
	theta float64
	sigma float64
	value float64
	min   float64
	max   float64
}

func newOU(mu, theta, sigma, min, max float64) *OUProcess {
	return &OUProcess{
		mu:    mu,
		theta: theta,
		sigma: sigma,
		value: mu + (rand.Float64()-0.5)*sigma,
		min:   min,
		max:   max,
	}
}

func (ou *OUProcess) step(dt float64) float64 {
	dW := rand.NormFloat64() * math.Sqrt(dt)
	ou.value += ou.theta*(ou.mu-ou.value)*dt + ou.sigma*dW
	if ou.value < ou.min {
		ou.value = ou.min
	}
	if ou.value > ou.max {
		ou.value = ou.max
	}
	return ou.value
}

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

type Config struct {
	GPUs []GPUConfig `yaml:"gpus"`
}

type GPUConfig struct {
	Model         string           `yaml:"model"`
	UUID          string           `yaml:"uuid"`
	MemoryGB      int              `yaml:"memory_gb"`
	DriverVersion string           `yaml:"driver_version"`
	Hostname      string           `yaml:"hostname"`
	Instances     []InstanceConfig `yaml:"instances"`
}

type InstanceConfig struct {
	Profile   string `yaml:"profile"`
	GIID      int    `yaml:"gi_id"`
	Namespace string `yaml:"namespace"`
	Pod       string `yaml:"pod"`
	Container string `yaml:"container"`
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if len(cfg.GPUs) == 0 {
		return nil, fmt.Errorf("config must define at least one GPU")
	}
	for i, gpu := range cfg.GPUs {
		if len(gpu.Instances) == 0 {
			return nil, fmt.Errorf("gpus[%d] must define at least one instance", i)
		}
	}
	return &cfg, nil
}

// ---------------------------------------------------------------------------
// MIG topology (runtime)
// ---------------------------------------------------------------------------

type MIGInstance struct {
	GPU       string
	UUID      string
	Device    string
	ModelName string
	Profile   string
	GIID      string
	Hostname  string
	Driver    string
	Namespace string
	Pod       string
	Container string
	FBTotal   float64

	grEngine *OUProcess
	smActive *OUProcess
	tensor   *OUProcess
	dram     *OUProcess
	fbUsed   *OUProcess
}

type PhysicalGPU struct {
	UUID    string
	temp    *OUProcess
	memTemp *OUProcess
	power   *OUProcess
	energy  float64
	clock   *OUProcess
	pcieTx  *OUProcess
	pcieRx  *OUProcess
	nvlink  float64
}

// ---------------------------------------------------------------------------
// Build simulation from config
// ---------------------------------------------------------------------------

func profileMemoryMiB(profile string) float64 {
	parts := strings.Split(profile, ".")
	if len(parts) != 2 {
		return 10240
	}
	memStr := strings.TrimSuffix(parts[1], "gb")
	mem, err := strconv.Atoi(memStr)
	if err != nil {
		return 10240
	}
	return float64(mem) * 1024
}

func instanceLoadFactor(idx, total int) float64 {
	if total == 1 {
		return 0.50
	}
	return 0.75 - float64(idx)*0.60/float64(total-1)
}

func buildSimulation(cfg *Config) ([]*PhysicalGPU, []*MIGInstance) {
	var pgpus []*PhysicalGPU
	var insts []*MIGInstance

	for gpuIdx, gpuCfg := range cfg.GPUs {
		pg := &PhysicalGPU{
			UUID:    gpuCfg.UUID,
			temp:    newOU(38, 0.05, 3.0, 20, 95),
			memTemp: newOU(36, 0.04, 2.5, 18, 90),
			power:   newOU(float64(gpuCfg.MemoryGB)*0.9, 0.08, 8.0, 30, 400),
			clock:   newOU(1350, 0.1, 120, 210, 1980),
			pcieTx:  newOU(5e8, 0.2, 2e8, 0, 25e9),
			pcieRx:  newOU(3e8, 0.2, 1.5e8, 0, 25e9),
		}
		pgpus = append(pgpus, pg)

		total := len(gpuCfg.Instances)
		for i, instCfg := range gpuCfg.Instances {
			fbTotal := profileMemoryMiB(instCfg.Profile)
			load := instanceLoadFactor(i, total)

			inst := &MIGInstance{
				GPU:       fmt.Sprintf("%d", gpuIdx),
				UUID:      gpuCfg.UUID,
				Device:    fmt.Sprintf("nvidia%d", gpuIdx),
				ModelName: gpuCfg.Model,
				Profile:   instCfg.Profile,
				GIID:      fmt.Sprintf("%d", instCfg.GIID),
				Hostname:  gpuCfg.Hostname,
				Driver:    gpuCfg.DriverVersion,
				Namespace: instCfg.Namespace,
				Pod:       instCfg.Pod,
				Container: instCfg.Container,
				FBTotal:   fbTotal,
				grEngine:  newOU(load, 0.3, 0.12, 0, 1),
				smActive:  newOU(load*0.8, 0.3, 0.10, 0, 1),
				tensor:    newOU(load*0.5, 0.2, 0.08, 0, 1),
				dram:      newOU(0.15+load*0.3, 0.2, 0.06, 0, 1),
				fbUsed:    newOU(fbTotal*(0.3+load*0.4), 0.05, fbTotal*0.02, 0, fbTotal),
			}
			insts = append(insts, inst)
		}
	}

	return pgpus, insts
}

// ---------------------------------------------------------------------------
// Simulation state
// ---------------------------------------------------------------------------

var (
	mu         sync.Mutex
	lastTick   = time.Now()
	tickerDone = make(chan struct{})

	physicalGPUs []*PhysicalGPU
	instances    []*MIGInstance
)

// ---------------------------------------------------------------------------
// Background ticker — advance OU processes every second
// ---------------------------------------------------------------------------

func startTicker() {
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				mu.Lock()
				now := time.Now()
				dt := now.Sub(lastTick).Seconds()
				lastTick = now

				for _, pg := range physicalGPUs {
					pg.temp.step(dt)
					pg.memTemp.step(dt)
					pg.power.step(dt)
					pg.energy += pg.power.value * dt * 1000
					pg.clock.step(dt)
					pg.pcieTx.step(dt)
					pg.pcieRx.step(dt)
					pg.nvlink += (pg.pcieTx.value + pg.pcieRx.value) * dt * 0.5
				}
				for _, inst := range instances {
					inst.grEngine.step(dt)
					inst.smActive.step(dt)
					inst.tensor.step(dt)
					inst.dram.step(dt)
					inst.fbUsed.step(dt)
				}
				mu.Unlock()
			case <-tickerDone:
				return
			}
		}
	}()
}

// ---------------------------------------------------------------------------
// HTTP handler
// ---------------------------------------------------------------------------

func labelsStr(inst *MIGInstance) string {
	pairs := []string{
		fmt.Sprintf(`gpu="%s"`, inst.GPU),
		fmt.Sprintf(`UUID="%s"`, inst.UUID),
		fmt.Sprintf(`device="%s"`, inst.Device),
		fmt.Sprintf(`modelName="%s"`, inst.ModelName),
		fmt.Sprintf(`GPU_I_PROFILE="%s"`, inst.Profile),
		fmt.Sprintf(`GPU_I_ID="%s"`, inst.GIID),
		fmt.Sprintf(`Hostname="%s"`, inst.Hostname),
		fmt.Sprintf(`DCGM_FI_DRIVER_VERSION="%s"`, inst.Driver),
	}
	if inst.Namespace != "" {
		pairs = append(pairs,
			fmt.Sprintf(`namespace="%s"`, inst.Namespace),
			fmt.Sprintf(`pod="%s"`, inst.Pod),
			fmt.Sprintf(`container="%s"`, inst.Container),
		)
	}
	return strings.Join(pairs, ",")
}

func findGPU(uuid string) *PhysicalGPU {
	for _, pg := range physicalGPUs {
		if pg.UUID == uuid {
			return pg
		}
	}
	return physicalGPUs[0]
}

type metricDef struct {
	name string
	help string
	typ  string
}

func metricsHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/metrics" {
		http.NotFound(w, r)
		return
	}

	mu.Lock()
	defer mu.Unlock()

	var b strings.Builder

	writeMetric(&b, metricDef{
		"DCGM_FI_PROF_GR_ENGINE_ACTIVE",
		"Ratio of time the graphics engine is active.",
		"gauge",
	}, func(inst *MIGInstance) string {
		return fmt.Sprintf("%.6f", inst.grEngine.value)
	})

	writeMetric(&b, metricDef{
		"DCGM_FI_PROF_SM_ACTIVE",
		"The ratio of cycles an SM has at least 1 warp assigned.",
		"gauge",
	}, func(inst *MIGInstance) string {
		return fmt.Sprintf("%.6f", inst.smActive.value)
	})

	writeMetric(&b, metricDef{
		"DCGM_FI_PROF_PIPE_TENSOR_ACTIVE",
		"Ratio of cycles the tensor pipe is active.",
		"gauge",
	}, func(inst *MIGInstance) string {
		return fmt.Sprintf("%.6f", inst.tensor.value)
	})

	writeMetric(&b, metricDef{
		"DCGM_FI_PROF_DRAM_ACTIVE",
		"Ratio of cycles the device memory interface is active.",
		"gauge",
	}, func(inst *MIGInstance) string {
		return fmt.Sprintf("%.6f", inst.dram.value)
	})

	writeMetric(&b, metricDef{
		"DCGM_FI_DEV_FB_USED",
		"Framebuffer memory used (in MiB).",
		"gauge",
	}, func(inst *MIGInstance) string {
		return fmt.Sprintf("%.0f", inst.fbUsed.value)
	})

	writeMetric(&b, metricDef{
		"DCGM_FI_DEV_FB_FREE",
		"Framebuffer memory free (in MiB).",
		"gauge",
	}, func(inst *MIGInstance) string {
		return fmt.Sprintf("%.0f", inst.FBTotal-inst.fbUsed.value)
	})

	writeMetric(&b, metricDef{
		"DCGM_FI_DEV_FB_RESERVED",
		"Framebuffer memory reserved (in MiB).",
		"gauge",
	}, func(inst *MIGInstance) string {
		return fmt.Sprintf("%.0f", inst.FBTotal*0.02)
	})

	writeMetric(&b, metricDef{
		"DCGM_FI_DEV_GPU_TEMP",
		"GPU temperature (in C).",
		"gauge",
	}, func(inst *MIGInstance) string {
		return fmt.Sprintf("%.0f", findGPU(inst.UUID).temp.value)
	})

	writeMetric(&b, metricDef{
		"DCGM_FI_DEV_POWER_USAGE",
		"Power draw (in W).",
		"gauge",
	}, func(inst *MIGInstance) string {
		return fmt.Sprintf("%.3f", findGPU(inst.UUID).power.value)
	})

	writeMetric(&b, metricDef{
		"DCGM_FI_DEV_SM_CLOCK",
		"SM clock frequency (in MHz).",
		"gauge",
	}, func(inst *MIGInstance) string {
		return fmt.Sprintf("%.0f", findGPU(inst.UUID).clock.value)
	})

	writeMetric(&b, metricDef{
		"DCGM_FI_DEV_MEM_CLOCK",
		"Memory clock frequency (in MHz).",
		"gauge",
	}, func(inst *MIGInstance) string {
		return "1593"
	})

	writeMetric(&b, metricDef{
		"DCGM_FI_DEV_MEMORY_TEMP",
		"Memory temperature (in C).",
		"gauge",
	}, func(inst *MIGInstance) string {
		return fmt.Sprintf("%.0f", findGPU(inst.UUID).memTemp.value)
	})

	writeMetric(&b, metricDef{
		"DCGM_FI_DEV_TOTAL_ENERGY_CONSUMPTION",
		"Total energy consumption since boot (in mJ).",
		"counter",
	}, func(inst *MIGInstance) string {
		return fmt.Sprintf("%.0f", findGPU(inst.UUID).energy)
	})

	writeMetric(&b, metricDef{
		"DCGM_FI_DEV_PCIE_REPLAY_COUNTER",
		"Total number of PCIe retries.",
		"counter",
	}, func(inst *MIGInstance) string {
		return "0"
	})

	writeMetric(&b, metricDef{
		"DCGM_FI_DEV_XID_ERRORS",
		"Value of the last XID error encountered.",
		"gauge",
	}, func(inst *MIGInstance) string {
		return "0"
	})

	writeMetric(&b, metricDef{
		"DCGM_FI_DEV_NVLINK_BANDWIDTH_TOTAL",
		"Total number of NVLink bandwidth counters for all lanes.",
		"counter",
	}, func(inst *MIGInstance) string {
		return fmt.Sprintf("%.0f", findGPU(inst.UUID).nvlink)
	})

	writeMetric(&b, metricDef{
		"DCGM_FI_DEV_VGPU_LICENSE_STATUS",
		"vGPU License status.",
		"gauge",
	}, func(inst *MIGInstance) string {
		return "1"
	})

	writeMetric(&b, metricDef{
		"DCGM_FI_DEV_UNCORRECTABLE_REMAPPED_ROWS",
		"Number of remapped rows for uncorrectable errors.",
		"counter",
	}, func(inst *MIGInstance) string {
		return "0"
	})

	writeMetric(&b, metricDef{
		"DCGM_FI_DEV_CORRECTABLE_REMAPPED_ROWS",
		"Number of remapped rows for correctable errors.",
		"counter",
	}, func(inst *MIGInstance) string {
		return "0"
	})

	writeMetric(&b, metricDef{
		"DCGM_FI_DEV_ROW_REMAP_FAILURE",
		"Whether remapping of rows has failed.",
		"gauge",
	}, func(inst *MIGInstance) string {
		return "0"
	})

	writeMetric(&b, metricDef{
		"DCGM_FI_PROF_PCIE_TX_BYTES",
		"The rate of data transmitted over the PCIe bus - in bytes per second.",
		"gauge",
	}, func(inst *MIGInstance) string {
		return fmt.Sprintf("%.0f", findGPU(inst.UUID).pcieTx.value)
	})

	writeMetric(&b, metricDef{
		"DCGM_FI_PROF_PCIE_RX_BYTES",
		"The rate of data received over the PCIe bus - in bytes per second.",
		"gauge",
	}, func(inst *MIGInstance) string {
		return fmt.Sprintf("%.0f", findGPU(inst.UUID).pcieRx.value)
	})

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprint(w, b.String())
}

func writeMetric(b *strings.Builder, def metricDef, valueFn func(*MIGInstance) string) {
	fmt.Fprintf(b, "# HELP %s %s\n", def.name, def.help)
	fmt.Fprintf(b, "# TYPE %s %s\n", def.name, def.typ)
	for _, inst := range instances {
		fmt.Fprintf(b, "%s{%s} %s\n", def.name, labelsStr(inst), valueFn(inst))
	}
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	defaultConfig := "config.yaml"
	if envPath := os.Getenv("CONFIG_PATH"); envPath != "" {
		defaultConfig = envPath
	}

	configPath := flag.String("config", defaultConfig, "path to config file")
	addr := flag.String("addr", ":9400", "listen address")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	physicalGPUs, instances = buildSimulation(cfg)

	startTicker()

	http.HandleFunc("/metrics", metricsHandler)
	log.Printf("Mock dcgm-exporter (MIG) listening on %s/metrics", *addr)
	log.Printf("Loaded config: %s", *configPath)
	log.Printf("Simulating %d MIG instance(s) on %d physical GPU(s)", len(instances), len(physicalGPUs))
	for _, inst := range instances {
		log.Printf("  GPU %s GI %s: %s (%s/%s)", inst.GPU, inst.GIID, inst.Profile, inst.Namespace, inst.Pod)
	}
	log.Fatal(http.ListenAndServe(*addr, nil))
}
