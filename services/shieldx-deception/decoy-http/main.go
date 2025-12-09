package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"shieldx/shared/shieldx-common/pkg/metrics"
)

// NOTE: This decoy strives to be realistic while staying lightweight under high load.
// Optimizations implemented:
// - HTTP server timeouts to protect from slowloris and resource exhaustion.
// - Request body is bounded and discarded to avoid memory bloat.
// - Shapeshifter banners are cached and refreshed periodically instead of per-request fetches.
// - Single http.Client with timeout for outbound calls.

var (
	httpClient = &http.Client{Timeout: 3 * time.Second}
	// atomic cache for server banners
	bannersCache atomic.Value // []string
)

func rootHandler(w http.ResponseWriter, r *http.Request) {
	// Bound and discard any request body to avoid leaks (e.g., large POSTs)
	const maxBody = 1 << 20 // 1 MiB
	if r.Body != nil {
		_ = r.ParseForm() // parse small forms if present (best-effort)
		_, _ = io.CopyN(io.Discard, r.Body, maxBody)
		_ = r.Body.Close()
	}

	// Realism: rotate a set of plausible banners and apply jitter to response time
	banners := currentBanners()
	if len(banners) == 0 {
		banners = []string{"nginx/1.23.4"}
	}
	w.Header().Set("Server", banners[rand.Intn(len(banners))])
	w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))
	// Jitter: controlled via env DECOY_JITTER_MS (0..1000)
	if j := getenvInt("DECOY_JITTER_MS", 120); j > 0 {
		time.Sleep(time.Duration(rand.Intn(j)) * time.Millisecond)
	}
	fmt.Fprintf(w, "Welcome to ShieldX decoy. Path: %s\n", r.URL.Path)
}

func healthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(200)
	_, _ = w.Write([]byte("ok"))
}

func main() {
	rand.Seed(time.Now().UnixNano())
	port := getenvInt("DECOY_PORT", 8082)
	mux := http.NewServeMux()
	reg := metrics.NewRegistry()
	mHits := metrics.NewCounter("decoy_http_hits_total", "Total decoy http hits")

	// initialize banners cache and background refresher if shapeshifter configured
	initBannerCache()

	mux.HandleFunc("/healthz", healthz)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { mHits.Inc(); rootHandler(w, r) })
	reg.Register(mHits)
	mux.Handle("/metrics", reg)

	addr := fmt.Sprintf(":%d", port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MiB
	}
	log.Printf("[decoy-http] listening on %s", addr)
	log.Fatal(srv.ListenAndServe())
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

// initBannerCache bootstraps cache and starts a periodic refresh when SHAPESHIFTER_URL is set.
func initBannerCache() {
	// set a sane default upfront
	bannersCache.Store([]string{"nginx/1.23.4", "Apache/2.4.57 (Unix)", "Caddy", "envoy"})
	url := os.Getenv("SHAPESHIFTER_URL")
	if url == "" {
		return
	}
	// initial fetch (best-effort)
	_ = refreshBanners(url)
	// periodic refresh
	interval := time.Duration(getenvInt("SHAPESHIFTER_REFRESH_SECONDS", 30)) * time.Second
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for range t.C {
			_ = refreshBanners(url)
		}
	}()
}

// currentBanners returns cached banners.
func currentBanners() []string {
	if v := bannersCache.Load(); v != nil {
		if arr, ok := v.([]string); ok && len(arr) > 0 {
			return arr
		}
	}
	return []string{"nginx/1.23.4"}
}

// refreshBanners fetches banners from Shapeshifter with a short timeout.
func refreshBanners(baseURL string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/config/banners", nil)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("bad status: %s", resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var m struct {
		ServerBanners []string `json:"server_banners"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	if len(m.ServerBanners) > 0 {
		bannersCache.Store(m.ServerBanners)
	}
	return nil
}
