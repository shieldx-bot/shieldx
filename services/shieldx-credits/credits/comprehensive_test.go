//go:build extended
// +build extended

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestOptimizedCacheLayer tests multi-tier caching
func TestOptimizedCacheLayer(t *testing.T) {
	t.Run("L1 Cache Hit", func(t *testing.T) {
		cache := NewOptimizedCacheLayer(nil, 100)
		
		// Set value in L1
		cache.localLRU.Set("bal:tenant1", int64(1000), 30*time.Second)
		
		// Should hit L1
		balance, err := cache.GetBalance(context.Background(), "tenant1", func() (int64, error) {
			t.Fatal("Should not call DB fallback")
			return 0, nil
		})
		
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		
		if balance != 1000 {
			t.Errorf("Expected balance 1000, got %d", balance)
		}
		
		stats := cache.GetStats()
		if stats["l1_hit_rate"].(float64) != 1.0 {
			t.Errorf("Expected 100%% L1 hit rate")
		}
	})
	
	t.Run("Cache Invalidation", func(t *testing.T) {
		cache := NewOptimizedCacheLayer(nil, 100)
		
		cache.localLRU.Set("bal:tenant1", int64(1000), 30*time.Second)
		cache.InvalidateBalance(context.Background(), "tenant1")
		
		// Should miss cache and call DB
		dbCalled := false
		_, err := cache.GetBalance(context.Background(), "tenant1", func() (int64, error) {
			dbCalled = true
			return 500, nil
		})
		
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		
		if !dbCalled {
			t.Error("Expected DB fallback to be called")
		}
	})
	
	t.Run("LRU Eviction", func(t *testing.T) {
		cache := NewLRUCache(2) // Small capacity
		
		cache.Set("key1", "value1", time.Minute)
		cache.Set("key2", "value2", time.Minute)
		cache.Set("key3", "value3", time.Minute) // Should evict key1
		
		if _, ok := cache.Get("key1"); ok {
			t.Error("key1 should have been evicted")
		}
		
		if _, ok := cache.Get("key2"); !ok {
			t.Error("key2 should still be in cache")
		}
		
		if _, ok := cache.Get("key3"); !ok {
			t.Error("key3 should be in cache")
		}
	})
}

// TestCircuitBreaker tests failure protection
func TestCircuitBreaker(t *testing.T) {
	t.Run("Circuit Opens After Max Failures", func(t *testing.T) {
		cb := &CircuitBreaker{
			maxFailures:  3,
			resetTimeout: 1 * time.Second,
			state:        CircuitClosed,
		}
		
		// Record failures
		for i := 0; i < 3; i++ {
			cb.RecordFailure()
		}
		
		// Circuit should be open
		if cb.GetState() != "open" {
			t.Errorf("Expected circuit to be open, got %s", cb.GetState())
		}
		
		// Requests should be blocked
		if cb.AllowRequest() {
			t.Error("Circuit breaker should block requests when open")
		}
	})
	
	t.Run("Circuit Resets After Timeout", func(t *testing.T) {
		cb := &CircuitBreaker{
			maxFailures:  3,
			resetTimeout: 100 * time.Millisecond,
			state:        CircuitClosed,
		}
		
		// Open circuit
		for i := 0; i < 3; i++ {
			cb.RecordFailure()
		}
		
		// Wait for reset
		time.Sleep(150 * time.Millisecond)
		
		// Should allow request (half-open)
		if !cb.AllowRequest() {
			t.Error("Circuit should allow request after timeout")
		}
		
		// Success should close circuit
		cb.RecordSuccess()
		if cb.GetState() != "closed" {
			t.Errorf("Expected circuit to be closed, got %s", cb.GetState())
		}
	})
}

// TestPaymentMasking tests PCI DSS compliance
func TestPaymentMasking(t *testing.T) {
	masker, err := NewSecurePaymentMasker("test-encryption-key-32-bytes-long")
	if err != nil {
		t.Fatalf("Failed to create masker: %v", err)
	}
	
	t.Run("Mask Credit Card Number", func(t *testing.T) {
		data := &PaymentData{
			CardNumber: "4532-1234-5678-9010",
			CVV:        "123",
			ExpiryDate: "12/25",
		}
		
		masked := masker.MaskPaymentData(data)
		
		if masked.CardNumberMasked != "****-****-****-9010" {
			t.Errorf("Unexpected masked card: %s", masked.CardNumberMasked)
		}
		
		if masked.CardBrand != "VISA" {
			t.Errorf("Expected VISA, got %s", masked.CardBrand)
		}
		
		if masked.LastFourDigits != "9010" {
			t.Errorf("Expected last four 9010, got %s", masked.LastFourDigits)
		}
	})
	
	t.Run("Encrypt and Decrypt Payment Data", func(t *testing.T) {
		original := &PaymentData{
			CardNumber: "4532123456789010",
			CVV:        "123",
			ExpiryDate: "12/25",
		}
		
		encrypted, err := masker.EncryptPaymentData(original)
		if err != nil {
			t.Fatalf("Encryption failed: %v", err)
		}
		
		// Encrypted data should be different
		if encrypted == "" {
			t.Error("Encrypted data is empty")
		}
		
		// Decrypt and verify
		decrypted, err := masker.DecryptPaymentData(encrypted)
		if err != nil {
			t.Fatalf("Decryption failed: %v", err)
		}
		
		if decrypted.CardNumber != original.CardNumber {
			t.Error("Decrypted card number doesn't match")
		}
		
		if decrypted.CVV != original.CVV {
			t.Error("Decrypted CVV doesn't match")
		}
	})
	
	t.Run("Sanitize Log Data", func(t *testing.T) {
		logEntry := "Payment processed: card=4532-1234-5678-9010, cvv=123, ssn=123-45-6789"
		
		sanitized := masker.SanitizeLogData(logEntry)
		
		// Should not contain raw card number
		if bytes.Contains([]byte(sanitized), []byte("4532-1234-5678-9010")) {
			t.Error("Log still contains raw card number")
		}
		
		// Should not contain raw SSN
		if bytes.Contains([]byte(sanitized), []byte("123-45-6789")) {
			t.Error("Log still contains raw SSN")
		}
		
		// Should contain masked version
		if !bytes.Contains([]byte(sanitized), []byte("****")) {
			t.Error("Log doesn't contain masked data")
		}
	})
	
	t.Run("Validate Luhn Algorithm", func(t *testing.T) {
		validCards := []string{
			"4532123456789010",  // VISA
			"5425233430109903",  // MasterCard
			"374245455400126",   // AMEX
		}
		
		for _, card := range validCards {
			if !validateLuhn(card) {
				t.Errorf("Valid card %s failed Luhn check", card)
			}
		}
		
		invalidCard := "1234567890123456"
		if validateLuhn(invalidCard) {
			t.Error("Invalid card passed Luhn check")
		}
	})
}

// TestAdvancedShadowEngine tests parallel evaluation
func TestAdvancedShadowEngine(t *testing.T) {
	t.Run("Parallel Evaluation", func(t *testing.T) {
		engine := NewAdvancedShadowEngine(4)
		defer engine.Shutdown()
		
		// Create test samples
		samples := make([]TrafficSample, 1000)
		for i := 0; i < 1000; i++ {
			samples[i] = TrafficSample{
				ID:       fmt.Sprintf("sample-%d", i),
				SourceIP: "192.168.1.1",
				IsAttack: i%10 == 0, // 10% attack rate
			}
		}
		
		ruleConfig := map[string]interface{}{
			"type":      "blacklist",
			"ips":       []interface{}{"192.168.1.1"},
		}
		
		start := time.Now()
		result, err := engine.EvaluateRule("test-rule", ruleConfig, samples)
		duration := time.Since(start)
		
		if err != nil {
			t.Fatalf("Evaluation failed: %v", err)
		}
		
		// Verify results
		total := result.TP + result.FP + result.TN + result.FN
		if total != 1000 {
			t.Errorf("Expected 1000 evaluations, got %d", total)
		}
		
		// Should complete in reasonable time
		if duration > 5*time.Second {
			t.Errorf("Evaluation too slow: %v", duration)
		}
		
		t.Logf("Evaluated 1000 samples in %v", duration)
		t.Logf("Precision: %.2f, Recall: %.2f, F1: %.2f", 
			result.Precision, result.Recall, result.F1Score)
	})
	
	t.Run("Cache Hit", func(t *testing.T) {
		engine := NewAdvancedShadowEngine(4)
		defer engine.Shutdown()
		
		samples := []TrafficSample{{ID: "test", SourceIP: "1.1.1.1"}}
		ruleConfig := map[string]interface{}{"type": "blacklist"}
		
		// First evaluation
		result1, _ := engine.EvaluateRule("cached-rule", ruleConfig, samples)
		
		// Second evaluation (should hit cache)
		start := time.Now()
		result2, _ := engine.EvaluateRule("cached-rule", ruleConfig, samples)
		cacheTime := time.Since(start)
		
		if cacheTime > 10*time.Millisecond {
			t.Errorf("Cache retrieval too slow: %v", cacheTime)
		}
		
		if result1.TP != result2.TP {
			t.Error("Cached result doesn't match original")
		}
	})
	
	t.Run("Metrics Aggregation", func(t *testing.T) {
		engine := NewAdvancedShadowEngine(4)
		defer engine.Shutdown()
		
		// Run multiple evaluations
		for i := 0; i < 5; i++ {
			samples := []TrafficSample{{ID: fmt.Sprintf("s%d", i), SourceIP: "1.1.1.1"}}
			engine.EvaluateRule(fmt.Sprintf("rule-%d", i), 
				map[string]interface{}{"type": "blacklist"}, samples)
		}
		
		metrics := engine.GetMetrics()
		
		if metrics["total_evaluations"].(int64) < 5 {
			t.Error("Metrics not properly aggregated")
		}
	})
}

// Integration tests
func TestCreditsServiceIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}
	
	t.Run("Consume Credits ACID", func(t *testing.T) {
		// This would require actual DB setup
		// Placeholder for integration test structure
		t.Skip("Requires database")
	})
	
	t.Run("Idempotency Key", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Mock idempotent response
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(CreditResponse{
				Success:       true,
				TransactionID: "txn-123",
				Balance:       5000,
			})
		}))
		defer server.Close()
		
		req := ConsumeRequest{
			TenantID:       "tenant1",
			Amount:         100,
			IdempotencyKey: "idem-key-123",
		}
		
		body, _ := json.Marshal(req)
		resp, err := http.Post(server.URL+"/credits/consume", "application/json", bytes.NewReader(body))
		
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200, got %d", resp.StatusCode)
		}
	})
}

// Benchmark tests
func BenchmarkCacheOperations(b *testing.B) {
	cache := NewLRUCache(1000)
	
	b.Run("Set", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key-%d", i%1000)
			cache.Set(key, int64(i), time.Minute)
		}
	})
	
	b.Run("Get", func(b *testing.B) {
		// Pre-populate
		for i := 0; i < 1000; i++ {
			cache.Set(fmt.Sprintf("key-%d", i), int64(i), time.Minute)
		}
		
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key-%d", i%1000)
			cache.Get(key)
		}
	})
}

func BenchmarkPaymentEncryption(b *testing.B) {
	masker, _ := NewSecurePaymentMasker("test-encryption-key-32-bytes-long")
	data := &PaymentData{
		CardNumber: "4532123456789010",
		CVV:        "123",
		ExpiryDate: "12/25",
	}
	
	b.Run("Encrypt", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			masker.EncryptPaymentData(data)
		}
	})
	
	b.Run("Decrypt", func(b *testing.B) {
		encrypted, _ := masker.EncryptPaymentData(data)
		b.ResetTimer()
		
		for i := 0; i < b.N; i++ {
			masker.DecryptPaymentData(encrypted)
		}
	})
}

func BenchmarkShadowEvaluation(b *testing.B) {
	engine := NewAdvancedShadowEngine(8)
	defer engine.Shutdown()
	
	samples := make([]TrafficSample, 1000)
	for i := 0; i < 1000; i++ {
		samples[i] = TrafficSample{
			ID:       fmt.Sprintf("sample-%d", i),
			SourceIP: "192.168.1.1",
			IsAttack: i%10 == 0,
		}
	}
	
	ruleConfig := map[string]interface{}{
		"type": "blacklist",
		"ips":  []interface{}{"192.168.1.1"},
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.EvaluateRule("bench-rule", ruleConfig, samples)
	}
}

// Load test helper
func LoadTestCreditsService(t *testing.T, concurrency int, duration time.Duration) {
	t.Helper()
	
	done := make(chan bool)
	errors := make(chan error, concurrency)
	
	start := time.Now()
	
	for i := 0; i < concurrency; i++ {
		go func(id int) {
			for time.Since(start) < duration {
				req := ConsumeRequest{
					TenantID: fmt.Sprintf("tenant-%d", id%10),
					Amount:   10,
				}
				
				// Simulate API call
				body, _ := json.Marshal(req)
				resp, err := http.Post("http://localhost:5004/credits/consume", 
					"application/json", bytes.NewReader(body))
				
				if err != nil {
					errors <- err
					continue
				}
				resp.Body.Close()
			}
			done <- true
		}(i)
	}
	
	// Wait for all workers
	for i := 0; i < concurrency; i++ {
		<-done
	}
	
	close(errors)
	errorCount := len(errors)
	
	t.Logf("Load test completed: %d errors out of %d concurrent workers over %v",
		errorCount, concurrency, duration)
}
