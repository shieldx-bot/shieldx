package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"time"
)

type ShapeshifterV2 struct {
	graph      *DeceptionGraph
	mimic      *FingerprintMimic
	antiDetect *AntiDetection
	reflection *ReflectionEngine
}

type DeceptionGraph struct {
	Nodes  map[string]*DeceptionNode `json:"nodes"`
	Bandit *MultiArmedBandit         `json:"bandit"`
	mu     sync.RWMutex
}

type DeceptionNode struct {
	ID            string                 `json:"id"`
	Type          string                 `json:"type"`
	Config        map[string]interface{} `json:"config"`
	Effectiveness float64                `json:"effectiveness"`
	Visits        int                    `json:"visits"`
	LastUsed      time.Time              `json:"last_used"`
}

type MultiArmedBandit struct {
	Arms      map[string]*BanditArm `json:"arms"`
	Algorithm string                `json:"algorithm"`
	Epsilon   float64               `json:"epsilon"`
}

type BanditArm struct {
	ID     string  `json:"id"`
	Pulls  int     `json:"pulls"`
	Reward float64 `json:"reward"`
	Value  float64 `json:"value"`
}

type FingerprintMimic struct {
	HTTPProfiles map[string]*HTTPProfile
}

type HTTPProfile struct {
	Server       string
	Headers      map[string]string
	ResponseTime time.Duration
}

type AntiDetection struct {
	jitterEnabled bool
}

type ReflectionEngine struct {
	mirrors map[string]*AttackerMirror
}

type AttackerMirror struct {
	AttackerIP string
	Exploits   []ExploitAttempt
}

type ExploitAttempt struct {
	CVE     string
	Payload []byte
}

func NewDeceptionGraph() *DeceptionGraph {
	return &DeceptionGraph{
		Nodes: make(map[string]*DeceptionNode),
		Bandit: &MultiArmedBandit{
			Arms:      make(map[string]*BanditArm),
			Algorithm: "ucb1",
			Epsilon:   0.1,
		},
	}
}

func NewFingerprintMimic() *FingerprintMimic {
	fm := &FingerprintMimic{
		HTTPProfiles: make(map[string]*HTTPProfile),
	}

	fm.HTTPProfiles["apache"] = &HTTPProfile{
		Server: "Apache/2.4.41 (Ubuntu)",
		Headers: map[string]string{
			"X-Powered-By": "PHP/7.4.3",
		},
		ResponseTime: 50 * time.Millisecond,
	}

	return fm
}

func NewAntiDetection() *AntiDetection {
	return &AntiDetection{jitterEnabled: true}
}

func NewReflectionEngine() *ReflectionEngine {
	return &ReflectionEngine{mirrors: make(map[string]*AttackerMirror)}
}

func (dg *DeceptionGraph) AddNode(node *DeceptionNode) {
	dg.mu.Lock()
	defer dg.mu.Unlock()

	dg.Nodes[node.ID] = node
	dg.Bandit.Arms[node.ID] = &BanditArm{ID: node.ID}
}

func (dg *DeceptionGraph) SelectOptimalDecoy(ctx context.Context) (*DeceptionNode, error) {
	dg.mu.Lock()
	defer dg.mu.Unlock()

	if len(dg.Nodes) == 0 {
		return nil, fmt.Errorf("no nodes available")
	}

	totalPulls := 0
	for _, arm := range dg.Bandit.Arms {
		totalPulls += arm.Pulls
	}

	bestScore := -math.Inf(1)
	var bestNode *DeceptionNode

	for nodeID, node := range dg.Nodes {
		arm := dg.Bandit.Arms[nodeID]
		if arm.Pulls == 0 {
			return node, nil
		}

		confidence := math.Sqrt(2 * math.Log(float64(totalPulls)) / float64(arm.Pulls))
		score := arm.Value + confidence

		if score > bestScore {
			bestScore = score
			bestNode = node
		}
	}

	return bestNode, nil
}

func (dg *DeceptionGraph) UpdateReward(nodeID string, reward float64) {
	dg.mu.Lock()
	defer dg.mu.Unlock()

	arm := dg.Bandit.Arms[nodeID]
	if arm != nil {
		arm.Pulls++
		arm.Reward += reward
		arm.Value = arm.Reward / float64(arm.Pulls)
	}
}

func (fm *FingerprintMimic) MimicHTTPServer(profile string) http.Handler {
	p := fm.HTTPProfiles[profile]
	if p == nil {
		p = fm.HTTPProfiles["apache"]
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", p.Server)
		for k, v := range p.Headers {
			w.Header().Set(k, v)
		}

		time.Sleep(p.ResponseTime)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body><h1>Welcome</h1></body></html>"))
	})
}

func (ad *AntiDetection) AddIOJitter(min, max time.Duration) {
	if !ad.jitterEnabled {
		return
	}
	jitter := min + time.Duration(rand.Int63n(int64(max-min)))
	time.Sleep(jitter)
}

func (re *ReflectionEngine) CreateMirror(attackerIP string) *AttackerMirror {
	mirror := &AttackerMirror{
		AttackerIP: attackerIP,
		Exploits:   make([]ExploitAttempt, 0),
	}
	re.mirrors[attackerIP] = mirror
	return mirror
}

func (am *AttackerMirror) ReflectExploit(payload []byte) []byte {
	exploit := ExploitAttempt{
		CVE:     "CVE-UNKNOWN",
		Payload: payload,
	}
	am.Exploits = append(am.Exploits, exploit)
	return []byte("REFLECTED_PAYLOAD")
}

func CreateWebServerDecoy() *DeceptionNode {
	return &DeceptionNode{
		ID:   fmt.Sprintf("web_decoy_%d", rand.Uint64()),
		Type: "web_server",
		Config: map[string]interface{}{
			"port":    80,
			"profile": "apache",
		},
	}
}

func main() {
	rand.Seed(time.Now().UnixNano())

	service := &ShapeshifterV2{
		graph:      NewDeceptionGraph(),
		mimic:      NewFingerprintMimic(),
		antiDetect: NewAntiDetection(),
		reflection: NewReflectionEngine(),
	}

	// Initialize decoys
	webDecoy := CreateWebServerDecoy()
	service.graph.AddNode(webDecoy)

	mux := http.NewServeMux()

	mux.HandleFunc("/select-decoy", func(w http.ResponseWriter, r *http.Request) {
		service.antiDetect.AddIOJitter(10*time.Millisecond, 100*time.Millisecond)

		node, err := service.graph.SelectOptimalDecoy(r.Context())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		if node.Type == "web_server" {
			profile := "apache"
			if p, ok := node.Config["profile"].(string); ok {
				profile = p
			}

			handler := service.mimic.MimicHTTPServer(profile)
			handler.ServeHTTP(w, r)

			reward := 0.8
			service.graph.UpdateReward(node.ID, reward)
		}
	})

	mux.HandleFunc("/reflect-exploit", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			AttackerIP string `json:"attacker_ip"`
			Payload    []byte `json:"payload"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		mirror := service.reflection.CreateMirror(req.AttackerIP)
		counter := mirror.ReflectExploit(req.Payload)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"reflected_payload": string(counter),
			"attacker_ip":       req.AttackerIP,
		})
	})

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	})

	addr := ":8084"
	if v := os.Getenv("SHAPESHIFTER_PORT"); v != "" {
		addr = ":" + v
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	log.Printf("[shapeshifter-v2] listening on %s", addr)
	log.Fatal(srv.ListenAndServe())
}
