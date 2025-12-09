//go:build integration
// +build integration

package credits

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// EVENT SOURCING TESTS
// =============================================================================

func TestEventSourcingEngine_WriteEvent(t *testing.T) {
	engine := NewEventSourcingEngine("postgresql://test", "redis://test")
	
	event := Event{
		ID:            uuid.New().String(),
		AggregateID:   "tenant-123",
		AggregateType: "credit_account",
		EventType:     "credit_purchased",
		Data: map[string]interface{}{
			"amount": 1000,
			"price":  10.00,
		},
		Version:   1,
		Timestamp: time.Now(),
	}
	
	ctx := context.Background()
	err := engine.WriteEvent(ctx, event)
	
	assert.NoError(t, err, "WriteEvent should not return error")
}

func TestEventSourcingEngine_Concurrency(t *testing.T) {
	engine := NewEventSourcingEngine("postgresql://test", "redis://test")
	
	aggregateID := "tenant-concurrent-" + uuid.New().String()
	numGoroutines := 100
	eventsPerGoroutine := 100
	
	var wg sync.WaitGroup
	errChan := make(chan error, numGoroutines)
	
	ctx := context.Background()
	
	start := time.Now()
	
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			
			for j := 0; j < eventsPerGoroutine; j++ {
				event := Event{
					ID:            uuid.New().String(),
					AggregateID:   aggregateID,
					AggregateType: "credit_account",
					EventType:     "credit_consumed",
					Data: map[string]interface{}{
						"amount":   1,
						"workerID": workerID,
						"eventNum": j,
					},
					Version:   int64(workerID*eventsPerGoroutine + j + 1),
					Timestamp: time.Now(),
				}
				
				if err := engine.WriteEvent(ctx, event); err != nil {
					errChan <- err
					return
				}
			}
		}(i)
	}
	
	wg.Wait()
	close(errChan)
	
	elapsed := time.Since(start)
	totalEvents := numGoroutines * eventsPerGoroutine
	throughput := float64(totalEvents) / elapsed.Seconds()
	
	t.Logf("Wrote %d events in %v (%.2f events/sec)", totalEvents, elapsed, throughput)
	
	// Check for errors
	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}
	
	assert.Empty(t, errors, "No errors should occur during concurrent writes")
	assert.Greater(t, throughput, 10000.0, "Throughput should exceed 10,000 events/sec")
}

func TestEventSourcingEngine_Snapshot(t *testing.T) {
	engine := NewEventSourcingEngine("postgresql://test", "redis://test")
	
	aggregateID := "tenant-snapshot-" + uuid.New().String()
	ctx := context.Background()
	
	// Write 150 events to trigger snapshot (threshold: 100)
	for i := 0; i < 150; i++ {
		event := Event{
			ID:            uuid.New().String(),
			AggregateID:   aggregateID,
			AggregateType: "credit_account",
			EventType:     "credit_consumed",
			Data:          map[string]interface{}{"amount": 1},
			Version:       int64(i + 1),
			Timestamp:     time.Now(),
		}
		
		err := engine.WriteEvent(ctx, event)
		require.NoError(t, err)
	}
	
	// Verify snapshot was created
	snapshot, err := engine.GetLatestSnapshot(ctx, aggregateID)
	assert.NoError(t, err)
	assert.NotNil(t, snapshot)
	assert.Equal(t, int64(100), snapshot.Version)
}

func TestEventSourcingEngine_Replay(t *testing.T) {
	engine := NewEventSourcingEngine("postgresql://test", "redis://test")
	
	aggregateID := "tenant-replay-" + uuid.New().String()
	ctx := context.Background()
	
	// Write 50 events
	expectedBalance := 5000
	for i := 0; i < 50; i++ {
		event := Event{
			ID:            uuid.New().String(),
			AggregateID:   aggregateID,
			AggregateType: "credit_account",
			EventType:     "credit_purchased",
			Data:          map[string]interface{}{"amount": 100},
			Version:       int64(i + 1),
			Timestamp:     time.Now(),
		}
		
		err := engine.WriteEvent(ctx, event)
		require.NoError(t, err)
	}
	
	// Replay aggregate state
	start := time.Now()
	state, err := engine.ReplayAggregate(ctx, aggregateID)
	replayTime := time.Since(start)
	
	assert.NoError(t, err)
	assert.Equal(t, expectedBalance, state["balance"])
	assert.Less(t, replayTime, 100*time.Millisecond, "Replay should complete in <100ms")
	
	t.Logf("Replayed 50 events in %v", replayTime)
}

func TestEventSourcingEngine_Idempotency(t *testing.T) {
	engine := NewEventSourcingEngine("postgresql://test", "redis://test")
	
	ctx := context.Background()
	idempotencyKey := uuid.New().String()
	
	command := Command{
		ID:             uuid.New().String(),
		AggregateID:    "tenant-idempotent",
		CommandType:    "purchase_credits",
		Data:           map[string]interface{}{"amount": 1000},
		IdempotencyKey: idempotencyKey,
	}
	
	// Execute command twice with same idempotency key
	err1 := engine.ExecuteCommand(ctx, command)
	err2 := engine.ExecuteCommand(ctx, command)
	
	assert.NoError(t, err1)
	assert.NoError(t, err2)
	
	// Verify only one event was written
	events, err := engine.GetEventsByAggregate(ctx, "tenant-idempotent")
	assert.NoError(t, err)
	assert.Equal(t, 1, len(events), "Should only have 1 event despite 2 command executions")
}

// =============================================================================
// SHARDING ENGINE TESTS
// =============================================================================

func TestShardingEngine_Get(t *testing.T) {
	shards := []ShardInfo{
		{ID: 0, DSN: "postgres://shard0"},
		{ID: 1, DSN: "postgres://shard1"},
		{ID: 2, DSN: "postgres://shard2"},
		{ID: 3, DSN: "postgres://shard3"},
	}
	
	engine := NewShardingEngine(shards, 256)
	
	ctx := context.Background()
	key := "user-12345"
	
	err := engine.Set(ctx, key, map[string]interface{}{"balance": 5000})
	require.NoError(t, err)
	
	value, err := engine.Get(ctx, key)
	assert.NoError(t, err)
	assert.Equal(t, 5000, value["balance"])
}

func TestShardingEngine_Distribution(t *testing.T) {
	shards := []ShardInfo{
		{ID: 0, DSN: "postgres://shard0"},
		{ID: 1, DSN: "postgres://shard1"},
		{ID: 2, DSN: "postgres://shard2"},
		{ID: 3, DSN: "postgres://shard3"},
	}
	
	engine := NewShardingEngine(shards, 256)
	
	// Count keys per shard
	distribution := make(map[int]int)
	numKeys := 10000
	
	for i := 0; i < numKeys; i++ {
		key := "key-" + uuid.New().String()
		shardID := engine.GetShardForKey(key)
		distribution[shardID]++
	}
	
	t.Logf("Key distribution across shards:")
	for shardID, count := range distribution {
		percentage := float64(count) / float64(numKeys) * 100
		t.Logf("  Shard %d: %d keys (%.2f%%)", shardID, count, percentage)
	}
	
	// Check balance - each shard should have roughly 25% ± 5%
	for _, count := range distribution {
		percentage := float64(count) / float64(numKeys) * 100
		assert.InDelta(t, 25.0, percentage, 5.0, "Shard distribution should be balanced")
	}
}

func TestShardingEngine_CrossShardTransaction(t *testing.T) {
	shards := []ShardInfo{
		{ID: 0, DSN: "postgres://shard0"},
		{ID: 1, DSN: "postgres://shard1"},
	}
	
	engine := NewShardingEngine(shards, 256)
	
	ctx := context.Background()
	
	// Find two keys that map to different shards
	var key1, key2 string
	for {
		k1 := "key-" + uuid.New().String()
		k2 := "key-" + uuid.New().String()
		
		if engine.GetShardForKey(k1) != engine.GetShardForKey(k2) {
			key1 = k1
			key2 = k2
			break
		}
	}
	
	// Set initial values
	engine.Set(ctx, key1, map[string]interface{}{"balance": 1000})
	engine.Set(ctx, key2, map[string]interface{}{"balance": 500})
	
	// Execute cross-shard transaction
	operations := []ShardOperation{
		{
			ShardID: engine.GetShardForKey(key1),
			Query:   "UPDATE shard_data SET value = value - 200 WHERE key = ?",
			Args:    []interface{}{key1},
		},
		{
			ShardID: engine.GetShardForKey(key2),
			Query:   "UPDATE shard_data SET value = value + 200 WHERE key = ?",
			Args:    []interface{}{key2},
		},
	}
	
	start := time.Now()
	err := engine.ExecuteCrossShardTx(ctx, operations)
	txTime := time.Since(start)
	
	assert.NoError(t, err)
	assert.Less(t, txTime, 500*time.Millisecond, "Cross-shard tx should complete in <500ms")
	
	// Verify balances
	val1, _ := engine.Get(ctx, key1)
	val2, _ := engine.Get(ctx, key2)
	
	assert.Equal(t, 800, val1["balance"])
	assert.Equal(t, 700, val2["balance"])
	
	t.Logf("Cross-shard transaction completed in %v", txTime)
}

func TestShardingEngine_HighConcurrency(t *testing.T) {
	shards := []ShardInfo{
		{ID: 0, DSN: "postgres://shard0"},
		{ID: 1, DSN: "postgres://shard1"},
		{ID: 2, DSN: "postgres://shard2"},
		{ID: 3, DSN: "postgres://shard3"},
	}
	
	engine := NewShardingEngine(shards, 256)
	
	numGoroutines := 100
	operationsPerGoroutine := 1000
	
	var wg sync.WaitGroup
	ctx := context.Background()
	
	start := time.Now()
	
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			
			for j := 0; j < operationsPerGoroutine; j++ {
				key := "key-" + uuid.New().String()
				engine.Set(ctx, key, map[string]interface{}{"value": j})
				engine.Get(ctx, key)
			}
		}()
	}
	
	wg.Wait()
	elapsed := time.Since(start)
	
	totalOps := numGoroutines * operationsPerGoroutine * 2 // Set + Get
	throughput := float64(totalOps) / elapsed.Seconds()
	
	t.Logf("Executed %d operations in %v (%.2f ops/sec)", totalOps, elapsed, throughput)
	assert.Greater(t, throughput, 100000.0, "Should achieve >100K ops/sec")
}

// =============================================================================
// MULTI-CLOUD DR TESTS
// =============================================================================

func TestMultiCloudDR_Replication(t *testing.T) {
	primary := DatabaseRegion{
		ID:       "aws-us-east-1",
		Provider: "aws",
		DSN:      "postgres://primary",
		Priority: 1,
	}
	
	secondary := DatabaseRegion{
		ID:       "azure-eastus",
		Provider: "azure",
		DSN:      "postgres://secondary",
		Priority: 2,
	}
	
	drSystem := NewMultiCloudDRSystem([]DatabaseRegion{primary, secondary})
	
	ctx := context.Background()
	
	// Write to primary
	change := DataChange{
		Operation: "INSERT",
		Table:     "credit_balances",
		PrimaryKey: "tenant-123",
		Data: map[string]interface{}{
			"tenant_id": "tenant-123",
			"balance":   5000,
		},
	}
	
	start := time.Now()
	err := drSystem.ReplicateChange(ctx, change)
	replicationTime := time.Since(start)
	
	assert.NoError(t, err)
	assert.Less(t, replicationTime, 100*time.Millisecond, "Replication should complete in <100ms")
	
	t.Logf("Replication completed in %v", replicationTime)
}

func TestMultiCloudDR_FailoverDetection(t *testing.T) {
	primary := DatabaseRegion{
		ID:       "aws-us-east-1",
		Provider: "aws",
		DSN:      "postgres://primary",
		Priority: 1,
	}
	
	secondary := DatabaseRegion{
		ID:       "azure-eastus",
		Provider: "azure",
		DSN:      "postgres://secondary",
		Priority: 2,
	}
	
	drSystem := NewMultiCloudDRSystem([]DatabaseRegion{primary, secondary})
	
	ctx := context.Background()
	
	// Simulate primary failure
	drSystem.SimulatePrimaryFailure()
	
	// Wait for failover detection (max 2 health check cycles = 60s)
	time.Sleep(70 * time.Second)
	
	activeRegion := drSystem.GetActiveRegion()
	assert.Equal(t, "azure-eastus", activeRegion.ID, "Should failover to secondary region")
}

func TestMultiCloudDR_FailoverRTO(t *testing.T) {
	t.Skip("Integration test - requires real database instances")
	
	primary := DatabaseRegion{
		ID:       "aws-us-east-1",
		Provider: "aws",
		DSN:      "postgres://primary",
		Priority: 1,
	}
	
	secondary := DatabaseRegion{
		ID:       "azure-eastus",
		Provider: "azure",
		DSN:      "postgres://secondary",
		Priority: 2,
	}
	
	drSystem := NewMultiCloudDRSystem([]DatabaseRegion{primary, secondary})
	
	ctx := context.Background()
	
	// Trigger failover
	start := time.Now()
	err := drSystem.Failover(ctx, "azure-eastus", "Primary region unresponsive")
	rto := time.Since(start)
	
	assert.NoError(t, err)
	assert.Less(t, rto, 5*time.Minute, "RTO should be <5 minutes")
	
	t.Logf("Failover completed in %v (RTO target: 5m)", rto)
}

func TestMultiCloudDR_ReplicationLag(t *testing.T) {
	primary := DatabaseRegion{
		ID:       "aws-us-east-1",
		Provider: "aws",
		DSN:      "postgres://primary",
		Priority: 1,
	}
	
	secondary := DatabaseRegion{
		ID:       "azure-eastus",
		Provider: "azure",
		DSN:      "postgres://secondary",
		Priority: 2,
	}
	
	drSystem := NewMultiCloudDRSystem([]DatabaseRegion{primary, secondary})
	
	ctx := context.Background()
	
	// Write multiple changes
	for i := 0; i < 100; i++ {
		change := DataChange{
			Operation:  "INSERT",
			Table:      "audit_log",
			PrimaryKey: "event-" + uuid.New().String(),
			Data:       map[string]interface{}{"event": i},
		}
		
		drSystem.ReplicateChange(ctx, change)
	}
	
	// Check replication lag
	lag := drSystem.GetReplicationLag(ctx, "aws-us-east-1", "azure-eastus")
	
	assert.Less(t, lag, 60*time.Second, "Replication lag should be <60s (RPO target)")
	
	t.Logf("Replication lag: %v", lag)
}

// =============================================================================
// ZERO-DOWNTIME DEPLOYMENT TESTS
// =============================================================================

func TestZeroDowntimeDeployment_CanaryStages(t *testing.T) {
	deployment := NewZeroDowntimeDeployment(
		"credits-service",
		"blue",
		"green",
	)
	
	ctx := context.Background()
	
	// Execute canary deployment
	stages := []int{1, 5, 25, 50, 100}
	
	for _, percentage := range stages {
		err := deployment.ShiftTraffic(ctx, percentage)
		assert.NoError(t, err)
		
		// Check health
		healthy := deployment.CheckHealth(ctx, "green")
		assert.True(t, healthy, "Green environment should be healthy")
		
		t.Logf("Canary stage %d%% - health check passed", percentage)
	}
}

func TestZeroDowntimeDeployment_AutoRollback(t *testing.T) {
	deployment := NewZeroDowntimeDeployment(
		"credits-service",
		"blue",
		"green",
	)
	
	ctx := context.Background()
	
	// Simulate failing health check
	deployment.SimulateGreenFailure()
	
	// Attempt deployment
	err := deployment.Deploy(ctx, "v2.0.0")
	
	assert.Error(t, err, "Deployment should fail due to health check")
	
	// Verify rollback occurred
	activeEnv := deployment.GetActiveEnvironment()
	assert.Equal(t, "blue", activeEnv, "Should rollback to blue")
	
	t.Log("Auto-rollback successful")
}

func TestZeroDowntimeDeployment_FeatureFlags(t *testing.T) {
	deployment := NewZeroDowntimeDeployment(
		"credits-service",
		"blue",
		"green",
	)
	
	ctx := context.Background()
	
	// Set feature flag
	err := deployment.SetFeatureFlag(ctx, "new_algorithm", true, 50)
	assert.NoError(t, err)
	
	// Evaluate flag for 1000 users
	enabledCount := 0
	for i := 0; i < 1000; i++ {
		userID := "user-" + uuid.New().String()
		enabled := deployment.EvaluateFeatureFlag(ctx, "new_algorithm", userID)
		if enabled {
			enabledCount++
		}
	}
	
	// Should be approximately 50% ± 5%
	percentage := float64(enabledCount) / 1000.0 * 100
	assert.InDelta(t, 50.0, percentage, 5.0, "Feature flag should affect ~50% of users")
	
	t.Logf("Feature flag enabled for %.2f%% of users", percentage)
}

// =============================================================================
// COMPLIANCE MONITORING TESTS
// =============================================================================

func TestComplianceMonitoring_SOC2Checks(t *testing.T) {
	system := NewComplianceMonitoringSystem()
	
	ctx := context.Background()
	
	// Run SOC 2 checks
	report := system.RunComplianceCheck(ctx, "SOC2")
	
	assert.GreaterOrEqual(t, report.Score, 90.0, "SOC 2 score should be ≥90%")
	assert.Equal(t, "compliant", report.Status)
	
	t.Logf("SOC 2 Compliance Score: %.2f%%", report.Score)
	t.Logf("Total Controls: %d", report.TotalControls)
	t.Logf("Compliant Controls: %d", report.CompliantControls)
}

func TestComplianceMonitoring_AllFrameworks(t *testing.T) {
	system := NewComplianceMonitoringSystem()
	
	ctx := context.Background()
	frameworks := []string{"SOC2", "ISO27001", "GDPR", "PCI_DSS"}
	
	for _, framework := range frameworks {
		report := system.RunComplianceCheck(ctx, framework)
		
		assert.GreaterOrEqual(t, report.Score, 85.0, 
			framework+" score should be ≥85%")
		
		t.Logf("%s: %.2f%% (%d/%d controls compliant)", 
			framework, report.Score, report.CompliantControls, report.TotalControls)
	}
}

func TestComplianceMonitoring_FindingDetection(t *testing.T) {
	system := NewComplianceMonitoringSystem()
	
	ctx := context.Background()
	
	// Simulate compliance violation
	system.SimulateViolation("access_control", "critical")
	
	// Run check
	report := system.RunComplianceCheck(ctx, "SOC2")
	
	assert.Greater(t, len(report.Findings), 0, "Should detect compliance finding")
	
	// Verify critical finding
	var hasCritical bool
	for _, finding := range report.Findings {
		if finding.Severity == "critical" {
			hasCritical = true
			break
		}
	}
	
	assert.True(t, hasCritical, "Should have at least one critical finding")
	
	t.Logf("Detected %d compliance findings", len(report.Findings))
}

func TestComplianceMonitoring_AutoRemediation(t *testing.T) {
	system := NewComplianceMonitoringSystem()
	
	ctx := context.Background()
	
	// Create remediable finding
	finding := ComplianceFinding{
		ID:             uuid.New().String(),
		ControlID:      "encryption_at_rest",
		Severity:       "high",
		Description:    "Database encryption not enabled",
		Recommendation: "Enable encryption on all databases",
		Status:         "open",
	}
	
	// Attempt auto-remediation
	err := system.AttemptRemediation(ctx, finding)
	
	assert.NoError(t, err, "Auto-remediation should succeed")
	
	// Verify finding resolved
	updatedFinding := system.GetFinding(ctx, finding.ID)
	assert.Equal(t, "resolved", updatedFinding.Status)
	
	t.Log("Auto-remediation successful")
}

// =============================================================================
// INTEGRATION TESTS
// =============================================================================

func TestFullStack_EndToEnd(t *testing.T) {
	t.Skip("Full integration test - requires all services")
	
	// Initialize all systems
	eventSourcing := NewEventSourcingEngine("postgres://test", "redis://test")
	sharding := NewShardingEngine([]ShardInfo{{ID: 0, DSN: "postgres://shard0"}}, 256)
	drSystem := NewMultiCloudDRSystem([]DatabaseRegion{
		{ID: "primary", DSN: "postgres://primary", Priority: 1},
	})
	deployment := NewZeroDowntimeDeployment("credits-service", "blue", "green")
	compliance := NewComplianceMonitoringSystem()
	
	ctx := context.Background()
	
	// Simulate full workflow
	tenantID := "tenant-" + uuid.New().String()
	
	// 1. Purchase credits (Event Sourcing)
	purchaseEvent := Event{
		ID:            uuid.New().String(),
		AggregateID:   tenantID,
		AggregateType: "credit_account",
		EventType:     "credit_purchased",
		Data:          map[string]interface{}{"amount": 10000},
		Version:       1,
		Timestamp:     time.Now(),
	}
	err := eventSourcing.WriteEvent(ctx, purchaseEvent)
	require.NoError(t, err)
	
	// 2. Store in shard
	err = sharding.Set(ctx, tenantID, map[string]interface{}{"balance": 10000})
	require.NoError(t, err)
	
	// 3. Replicate to DR
	change := DataChange{
		Operation:  "INSERT",
		Table:      "credit_balances",
		PrimaryKey: tenantID,
		Data:       map[string]interface{}{"balance": 10000},
	}
	err = drSystem.ReplicateChange(ctx, change)
	require.NoError(t, err)
	
	// 4. Check compliance
	report := compliance.RunComplianceCheck(ctx, "SOC2")
	assert.GreaterOrEqual(t, report.Score, 90.0)
	
	// 5. Verify deployment health
	healthy := deployment.CheckHealth(ctx, "blue")
	assert.True(t, healthy)
	
	t.Log("Full stack integration test passed")
}

// =============================================================================
// BENCHMARK TESTS
// =============================================================================

func BenchmarkEventSourcing_WriteEvent(b *testing.B) {
	engine := NewEventSourcingEngine("postgres://test", "redis://test")
	ctx := context.Background()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		event := Event{
			ID:            uuid.New().String(),
			AggregateID:   "bench-tenant",
			AggregateType: "credit_account",
			EventType:     "credit_consumed",
			Data:          map[string]interface{}{"amount": 1},
			Version:       int64(i + 1),
			Timestamp:     time.Now(),
		}
		
		engine.WriteEvent(ctx, event)
	}
}

func BenchmarkSharding_Get(b *testing.B) {
	shards := []ShardInfo{
		{ID: 0, DSN: "postgres://shard0"},
		{ID: 1, DSN: "postgres://shard1"},
	}
	
	engine := NewShardingEngine(shards, 256)
	ctx := context.Background()
	
	key := "bench-key"
	engine.Set(ctx, key, map[string]interface{}{"value": 123})
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.Get(ctx, key)
	}
}

func BenchmarkDR_Replication(b *testing.B) {
	primary := DatabaseRegion{ID: "primary", DSN: "postgres://primary", Priority: 1}
	secondary := DatabaseRegion{ID: "secondary", DSN: "postgres://secondary", Priority: 2}
	
	drSystem := NewMultiCloudDRSystem([]DatabaseRegion{primary, secondary})
	ctx := context.Background()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		change := DataChange{
			Operation:  "UPDATE",
			Table:      "test",
			PrimaryKey: "key",
			Data:       map[string]interface{}{"value": i},
		}
		
		drSystem.ReplicateChange(ctx, change)
	}
}
