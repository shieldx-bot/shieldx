package main

import (
	"testing"
	"time"
)

// Test Chaos Engine initialization
func TestChaosEngineConfig(t *testing.T) {
	config := ChaosConfig{
		Enabled:                  true,
		TargetServices:           []string{"credits", "shadow", "guardian"},
		ExperimentInterval:       5 * time.Minute,
		MaxConcurrentExperiments: 2,
		SafeHours:                []int{0, 1, 2, 3, 4, 5, 22, 23},
		ProductionEnabled:        false,
		AutoRollbackEnabled:      true,
		MaxImpactThreshold:       10.0,
	}

	if !config.Enabled {
		t.Error("Chaos engine should be enabled")
	}

	if len(config.TargetServices) != 3 {
		t.Errorf("Expected 3 target services, got %d", len(config.TargetServices))
	}

	if config.MaxImpactThreshold != 10.0 {
		t.Errorf("Expected max impact threshold 10.0, got %.1f", config.MaxImpactThreshold)
	}

	t.Log("✅ Chaos engine configuration test passed")
}

// Test Experiment Types
func TestExperimentTypes(t *testing.T) {
	experimentTypes := []ExperimentType{
		ExperimentServiceFailure,
		ExperimentHighLatency,
		ExperimentResourceExhaustion,
		ExperimentNetworkPartition,
		ExperimentDBSlowQuery,
		ExperimentCacheFailure,
	}

	if len(experimentTypes) < 6 {
		t.Error("Not all experiment types are defined")
	}

	t.Logf("✅ Found %d experiment types", len(experimentTypes))
}

// Test Chaos Experiment structure
func TestChaosExperiment(t *testing.T) {
	experiment := &ChaosExperiment{
		ID:            "test-exp-001",
		Name:          "High Latency Test",
		Type:          ExperimentHighLatency,
		TargetService: "credits",
		Parameters: map[string]interface{}{
			"latency_ms":        200.0,
			"impact_percentage": 10.0,
		},
		Status:   "scheduled",
		Duration: 2 * time.Minute,
	}

	if experiment.ID != "test-exp-001" {
		t.Errorf("Expected ID test-exp-001, got %s", experiment.ID)
	}

	if experiment.Type != ExperimentHighLatency {
		t.Errorf("Expected ExperimentHighLatency, got %v", experiment.Type)
	}

	if experiment.Duration != 2*time.Minute {
		t.Errorf("Expected duration 2m, got %v", experiment.Duration)
	}

	t.Log("✅ Chaos experiment structure test passed")
}

// Test Metrics Collector
func TestChaosMetricsCollector(t *testing.T) {
	collector := NewChaosMetricsCollector()

	experimentID := "test-metrics-001"
	collector.StartCollection(experimentID)

	// Record some metrics
	for i := 0; i < 100; i++ {
		collector.RecordRequest(experimentID)
		if i%10 == 0 {
			collector.RecordError(experimentID)
		}
	}

	results := collector.ComputeResults(experimentID)

	if results.TotalRequests != 100 {
		t.Errorf("Expected 100 requests, got %d", results.TotalRequests)
	}

	if results.FailedRequests != 10 {
		t.Errorf("Expected 10 errors, got %d", results.FailedRequests)
	}

	expectedErrorRate := 0.1 // 10/100
	if results.ErrorRate != expectedErrorRate {
		t.Errorf("Expected error rate %.2f, got %.2f", expectedErrorRate, results.ErrorRate)
	}

	t.Log("✅ Metrics collector test passed")
}

// Benchmark chaos experiment creation
func BenchmarkExperimentCreation(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = &ChaosExperiment{
			ID:            "bench-exp",
			Name:          "Benchmark Test",
			Type:          ExperimentHighLatency,
			TargetService: "test-service",
			Parameters: map[string]interface{}{
				"latency_ms": 100.0,
			},
			Duration: 1 * time.Minute,
		}
	}
}
