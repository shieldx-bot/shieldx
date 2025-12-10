package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"shieldx/shared/shieldx-common/pkg/ledger"
	"shieldx/shared/shieldx-common/pkg/metrics"
	"shieldx/shared/shieldx-common/pkg/sandbox"

	"github.com/docker/docker/api/types/container"
	imagetypes "github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

type spawnRequest struct {
	Tenant string `json:"tenant"`
	Kind   string `json:"kind"` // http, ssh, etc.
}

type spawnResponse struct {
	DecoyID string `json:"decoyId"`
	Kind    string `json:"kind"`
	Addr    string `json:"addr"`
}

type analyzeRequest struct {
	DecoyID string `json:"decoyId"`
	Event   string `json:"event"`
	Payload string `json:"payload,omitempty"`
}

type decoyTemplate struct {
	Name          string
	Image         string
	ContainerPort nat.Port
}

type decoyInstance struct {
	ID        string
	Tenant    string
	Kind      string
	Addr      string
	Template  string
	ExpiresAt time.Time
	DockerID  string
}

var (
	serviceName = "decoy-manager"
	ledgerPath  = "data/ledger-decoy.log"
	reg         = metrics.NewRegistry()
	mSpawn      = metrics.NewCounter("decoy_spawn_total", "Total decoy spawns")
	mAnalyze    = metrics.NewCounter("decoy_analyze_total", "Total decoy analyze events")
	mRunning    = metrics.NewCounter("decoy_running_total", "Number of running decoys (docker)")

	useDocker = os.Getenv("DECOY_DOCKER") == "1"
	dockerCli *client.Client

	templates = []decoyTemplate{
		{Name: "http_nginxdemo", Image: getenv("DECOY_IMAGE_HELLO", "nginxdemos/hello:plain-text"), ContainerPort: nat.Port("80/tcp")},
		{Name: "http_caddy", Image: getenv("DECOY_IMAGE_CADDY", "caddy:alpine"), ContainerPort: nat.Port("80/tcp")},
		{Name: "http_apache", Image: getenv("DECOY_IMAGE_APACHE", "httpd:alpine"), ContainerPort: nat.Port("80/tcp")},
	}

	// bandit stats
	tmplStats = struct {
		mu    sync.Mutex
		pulls map[string]int
		wins  map[string]int
	}{pulls: map[string]int{}, wins: map[string]int{}}

	// registry
	instances = struct {
		mu   sync.Mutex
		byID map[string]decoyInstance
	}{byID: map[string]decoyInstance{}}
)

func getenv(k, def string) string {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	return v
}
func getenvInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func main() {
	rand.Seed(time.Now().UnixNano())
	if useDocker {
		cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			log.Printf("[decoy-manager] docker client error: %v (fallback to local decoy)", err)
			useDocker = false
		} else {
			dockerCli = cli
		}
	}
	port := getenvInt("DECOY_MGR_PORT", 8083)
	ttlSec := getenvInt("DECOY_TTL_SECONDS", 900)
	mux := http.NewServeMux()

	mux.HandleFunc("/spawn", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405)
			return
		}
		// bound body size to protect from abuse
		r.Body = http.MaxBytesReader(w, r.Body, 4<<10) // 4 KiB
		var req spawnRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", 400)
			return
		}
		if req.Kind == "" {
			req.Kind = "http"
		}
		id := fmt.Sprintf("dcy_%d", time.Now().UnixNano())
		var addr, dockerID, tmplName string
		if useDocker && req.Kind == "http" {
			tmpl := chooseTemplate()
			hostPort, err := pickFreePort()
			if err != nil {
				http.Error(w, "no free port", 500)
				return
			}
			// short timeout on docker operations
			ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
			defer cancel()
			dockerID, err = spawnHTTPContainer(ctx, tmpl.Image, tmpl.ContainerPort, hostPort, map[string]string{
				"shieldx.decoy": "1", "shieldx.tenant": req.Tenant, "shieldx.template": tmpl.Name, "shieldx.id": id,
			})
			if err != nil {
				log.Printf("spawn docker: %v", err)
				http.Error(w, "spawn error", 500)
				return
			}
			addr = fmt.Sprintf("127.0.0.1:%d", hostPort)
			tmplName = tmpl.Name
			mRunning.Add(1)
		} else {
			addr = fmt.Sprintf("127.0.0.1:%d", getenvInt("DECOY_PORT", 8082))
			tmplName = "local_decoy_http"
		}
		inst := decoyInstance{ID: id, Tenant: req.Tenant, Kind: req.Kind, Addr: addr, Template: tmplName, ExpiresAt: time.Now().Add(time.Duration(ttlSec) * time.Second), DockerID: dockerID}
		instances.mu.Lock()
		instances.byID[id] = inst
		instances.mu.Unlock()
		_ = ledger.AppendJSONLine(ledgerPath, serviceName, "spawn", map[string]any{"decoyId": id, "kind": req.Kind, "tenant": req.Tenant, "addr": addr, "tmpl": tmplName})
		mSpawn.Inc()
		_ = json.NewEncoder(w).Encode(spawnResponse{DecoyID: id, Kind: req.Kind, Addr: addr})
	})

	mux.HandleFunc("/analyze", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405)
			return
		}
		// bound body (payload can be large, cap at 1MB)
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		var req analyzeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", 400)
			return
		}
		// Write a sandbox artifact log file per decoy id
		_ = os.MkdirAll(filepath.Join("data", "sandbox"), 0o755)
		path := filepath.Join("data", "sandbox", fmt.Sprintf("%s-%d.log", req.DecoyID, time.Now().Unix()))
		// Run sandbox via pluggable runner (noop or docker), no-network
		runner := sandbox.NewFromEnv()
		// enforce analysis timeout
		ctx, cancel := context.WithTimeout(r.Context(), time.Duration(getenvInt("SANDBOX_TIMEOUT_SECONDS", 5))*time.Second)
		defer cancel()
		out, _ := runner.Run(ctx, req.Payload)
		// Optional WASM analyzer: provide module bytes via env path SANDBOX_WASM_PATH and export name SANDBOX_WASM_FUNC
		if path := os.Getenv("SANDBOX_WASM_PATH"); path != "" {
			if b, err := os.ReadFile(path); err == nil {
				fn := os.Getenv("SANDBOX_WASM_FUNC")
				if fn == "" {
					fn = "analyze"
				}
				wr := sandbox.NewWASMRunner(b, fn, 2*time.Second)
				if s, err2 := wr.Run(r.Context(), req.Payload); err2 == nil {
					out += "\nWASM:" + s
				}
			}
		}
		_ = os.WriteFile(path, []byte(fmt.Sprintf("%s\n%s\n", req.Event, out)), 0o600)
		_ = ledger.AppendJSONLine(ledgerPath, serviceName, "analyze", map[string]any{"decoyId": req.DecoyID, "event": req.Event})
		mAnalyze.Inc()
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("/list", func(w http.ResponseWriter, r *http.Request) {
		instances.mu.Lock()
		defer instances.mu.Unlock()
		var arr []decoyInstance
		for _, v := range instances.byID {
			arr = append(arr, v)
		}
		_ = json.NewEncoder(w).Encode(arr)
	})

	// Background pruning loop + simple autoscale (density) based on analyze rate
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for range t.C {
			now := time.Now()
			var toStop []decoyInstance
			instances.mu.Lock()
			for _, inst := range instances.byID {
				if now.After(inst.ExpiresAt) {
					toStop = append(toStop, inst)
					delete(instances.byID, inst.ID)
				}
			}
			instances.mu.Unlock()
			for _, inst := range toStop {
				stopContainer(inst.DockerID)
			}
			// naive autoscale: if analyze rate high, pre-spawn an extra decoy
			if getenvInt("DECOY_AUTOSCALE", 0) == 1 {
				// Count recent analyze events by reading ledger lines quickly is omitted; spawn one opportunistically
				if rand.Intn(5) == 0 {
					_, _ = http.Post("http://localhost:"+strconv.Itoa(getenvInt("DECOY_MGR_PORT", 8083))+"/spawn", "application/json", bytes.NewReader([]byte(`{"tenant":"auto","kind":"http"}`)))
				}
			}
		}
	}()

	reg.Register(mSpawn)
	reg.Register(mAnalyze)
	reg.Register(mRunning)
	mux.Handle("/metrics", reg)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200); _, _ = w.Write([]byte("ok")) })

	addr := fmt.Sprintf(":%d", port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      20 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	log.Printf("[decoy-manager] listening on %s (docker=%v)", addr, useDocker)
	log.Fatal(srv.ListenAndServe())
}

// chooseTemplate picks a template with epsilon-greedy bandit
func chooseTemplate() decoyTemplate {
	eps := 0.2
	if rand.Float64() < eps {
		return templates[rand.Intn(len(templates))]
	}
	tmplStats.mu.Lock()
	defer tmplStats.mu.Unlock()
	// compute rates; pick best
	bestIdx := 0
	bestRate := -1.0
	for i, t := range templates {
		pulls := float64(tmplStats.pulls[t.Name] + 1)
		wins := float64(tmplStats.wins[t.Name])
		rate := wins / pulls
		if rate > bestRate {
			bestRate = rate
			bestIdx = i
		}
	}
	name := templates[bestIdx].Name
	tmplStats.pulls[name] = tmplStats.pulls[name] + 1
	return templates[bestIdx]
}

func pickFreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	a := l.Addr().(*net.TCPAddr)
	return a.Port, nil
}

func spawnHTTPContainer(ctx context.Context, image string, cport nat.Port, hostPort int, labels map[string]string) (string, error) {
	if dockerCli == nil {
		return "", fmt.Errorf("docker not available")
	}
	// ensure image
	rc, err := dockerCli.ImagePull(ctx, image, imagetypes.PullOptions{})
	if err == nil {
		io.Copy(io.Discard, rc)
		rc.Close()
	} // ignore pull errors if already present
	// port bindings
	pb := nat.PortMap{cport: []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: strconv.Itoa(hostPort)}}}
	resp, err := dockerCli.ContainerCreate(ctx, &container.Config{
		Image:        image,
		ExposedPorts: nat.PortSet{cport: struct{}{}},
		Labels:       labels,
	}, &container.HostConfig{
		PortBindings: pb,
		AutoRemove:   true,
	}, nil, nil, "")
	if err != nil {
		return "", err
	}
	if err := dockerCli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", err
	}
	return resp.ID, nil
}

func stopContainer(id string) {
	if dockerCli == nil || id == "" {
		return
	}
	// Use StopOptions with Timeout seconds
	sec := 2
	_ = dockerCli.ContainerStop(context.Background(), id, container.StopOptions{Timeout: &sec})
	mRunning.Add(^uint64(0)) // decrement by 1
}

// runSandbox removed in favor of pluggable sandbox.Runner
