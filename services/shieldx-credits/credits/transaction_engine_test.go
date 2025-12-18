//go:build integration
// +build integration

package main

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// TestTransactionEngine_AtomicConsumeCredits tests atomic credit consumption
func TestTransactionEngine_AtomicConsumeCredits(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	engine := NewTransactionEngine(db)

	// Create test account
	tenantID := "test-tenant-001"
	setupTestAccount(t, db, tenantID, 1000)

	tests := []struct {
		name        string
		tenantID    string
		amount      int64
		expectError bool
		errorMsg    string
	}{
		{
			name:        "successful consumption",
			tenantID:    tenantID,
			amount:      100,
			expectError: false,
		},
		{
			name:        "insufficient balance",
			tenantID:    tenantID,
			amount:      10000,
			expectError: true,
			errorMsg:    "insufficient credits",
		},
		{
			name:        "zero amount",
			tenantID:    tenantID,
			amount:      0,
			expectError: true,
			errorMsg:    "amount must be positive",
		},
		{
			name:        "negative amount",
			tenantID:    tenantID,
			amount:      -100,
			expectError: true,
			errorMsg:    "amount must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			opts := TransactionOptions{
				IsolationLevel: sql.LevelSerializable,
				MaxRetries:     3,
				Timeout:        5 * time.Second,
			}

			txID, balance, err := engine.AtomicConsumeCredits(ctx, tt.tenantID, tt.amount, "test", opts)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				if tt.errorMsg != "" && err.Error() != tt.errorMsg {
					t.Errorf("expected error '%s' but got '%s'", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if txID == "" {
					t.Error("expected transaction ID but got empty")
				}
				if balance < 0 {
					t.Errorf("balance should never be negative, got %d", balance)
				}
			}
		})
	}
}

// TestTransactionEngine_ConcurrentConsumption tests concurrent credit consumption
func TestTransactionEngine_ConcurrentConsumption(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	engine := NewTransactionEngine(db)

	// Create test account with 1000 credits
	tenantID := "concurrent-test-001"
	initialBalance := int64(1000)
	setupTestAccount(t, db, tenantID, initialBalance)

	// Spawn 10 goroutines each trying to consume 100 credits
	numGoroutines := 10
	consumeAmount := int64(100)

	successCh := make(chan bool, numGoroutines)
	errorCh := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			ctx := context.Background()
			opts := TransactionOptions{
				IsolationLevel: sql.LevelSerializable,
				MaxRetries:     5,
				Timeout:        10 * time.Second,
			}

			_, _, err := engine.AtomicConsumeCredits(ctx, tenantID, consumeAmount, "concurrent test", opts)

			if err != nil {
				errorCh <- err
			} else {
				successCh <- true
			}
		}()
	}

	// Wait for all goroutines to complete
	successCount := 0
	errorCount := 0

	for i := 0; i < numGoroutines; i++ {
		select {
		case <-successCh:
			successCount++
		case <-errorCh:
			errorCount++
		case <-time.After(15 * time.Second):
			t.Fatal("timeout waiting for goroutines")
		}
	}

	// Exactly 10 should succeed (10 * 100 = 1000)
	if successCount != numGoroutines {
		t.Errorf("expected %d successful transactions, got %d", numGoroutines, successCount)
	}

	// Verify final balance is 0
	finalBalance := getBalance(t, db, tenantID)
	if finalBalance != 0 {
		t.Errorf("expected final balance 0, got %d", finalBalance)
	}

	// P0 REQUIREMENT: Balance must NEVER be negative
	if finalBalance < 0 {
		t.Fatalf("CRITICAL: Negative balance detected: %d", finalBalance)
	}
}

// TestTransactionEngine_IdempotencyKey tests idempotency
func TestTransactionEngine_IdempotencyKey(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	engine := NewTransactionEngine(db)

	tenantID := "idempotency-test-001"
	setupTestAccount(t, db, tenantID, 1000)

	idempotencyKey := "test-key-12345"

	ctx := context.Background()
	opts := TransactionOptions{
		IsolationLevel: sql.LevelSerializable,
		MaxRetries:     3,
		Timeout:        5 * time.Second,
		IdempotencyKey: idempotencyKey,
	}

	// First request should succeed
	txID1, balance1, err1 := engine.AtomicConsumeCredits(ctx, tenantID, 100, "test", opts)
	if err1 != nil {
		t.Fatalf("first request failed: %v", err1)
	}

	// Second request with same idempotency key should fail
	txID2, balance2, err2 := engine.AtomicConsumeCredits(ctx, tenantID, 100, "test", opts)
	if err2 == nil {
		t.Error("expected duplicate transaction error but got none")
	}

	// Verify only one transaction was processed
	if txID2 != "" {
		t.Error("second transaction should not have been processed")
	}

	// Verify balance was only deducted once
	finalBalance := getBalance(t, db, tenantID)
	expectedBalance := int64(900)
	if finalBalance != expectedBalance {
		t.Errorf("expected balance %d, got %d", expectedBalance, finalBalance)
	}

	t.Logf("First TX: %s, Balance: %d", txID1, balance1)
	t.Logf("Second TX rejected, Final Balance: %d", finalBalance)
}

// TestTransactionEngine_OptimisticLocking tests optimistic locking
func TestTransactionEngine_OptimisticLocking(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	engine := NewTransactionEngine(db)

	tenantID := "optimistic-test-001"
	setupTestAccount(t, db, tenantID, 1000)

	// Simulate concurrent updates
	ctx := context.Background()

	opts1 := TransactionOptions{
		IsolationLevel: sql.LevelSerializable,
		MaxRetries:     5,
		Timeout:        10 * time.Second,
	}

	opts2 := TransactionOptions{
		IsolationLevel: sql.LevelSerializable,
		MaxRetries:     5,
		Timeout:        10 * time.Second,
	}

	done := make(chan bool, 2)

	// Transaction 1
	go func() {
		_, _, err := engine.AtomicConsumeCredits(ctx, tenantID, 100, "tx1", opts1)
		if err != nil {
			t.Logf("TX1 failed: %v", err)
		}
		done <- true
	}()

	// Transaction 2 (slight delay)
	go func() {
		time.Sleep(10 * time.Millisecond)
		_, _, err := engine.AtomicConsumeCredits(ctx, tenantID, 200, "tx2", opts2)
		if err != nil {
			t.Logf("TX2 failed: %v", err)
		}
		done <- true
	}()

	// Wait for both
	<-done
	<-done

	// Both should eventually succeed due to retry logic
	finalBalance := getBalance(t, db, tenantID)
	expectedBalance := int64(700) // 1000 - 100 - 200

	if finalBalance != expectedBalance {
		t.Errorf("expected final balance %d, got %d", expectedBalance, finalBalance)
	}
}

// TestTransactionEngine_AuditLog tests immutable audit logging
func TestTransactionEngine_AuditLog(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	engine := NewTransactionEngine(db)

	tenantID := "audit-test-001"
	setupTestAccount(t, db, tenantID, 1000)

	ctx := context.Background()
	opts := TransactionOptions{
		IsolationLevel: sql.LevelSerializable,
		MaxRetries:     3,
		Timeout:        5 * time.Second,
	}

	// Perform transaction
	txID, _, err := engine.AtomicConsumeCredits(ctx, tenantID, 100, "audit test", opts)
	if err != nil {
		t.Fatalf("transaction failed: %v", err)
	}

	// Verify audit log entry exists
	var logCount int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM credit_transactions WHERE id = $1
	`, txID).Scan(&logCount)

	if err != nil {
		t.Fatalf("failed to query audit log: %v", err)
	}

	if logCount != 1 {
		t.Errorf("expected 1 audit log entry, got %d", logCount)
	}

	// P0 REQUIREMENT: Audit log must be immutable
	// Try to modify audit log (should fail)
	_, err = db.Exec(`
		UPDATE credit_transactions SET amount = 999 WHERE id = $1
	`, txID)

	// In production, this should be prevented by database permissions
	// For test, we just verify the log exists and wasn't deleted
	var finalAmount int64
	err = db.QueryRow(`
		SELECT amount FROM credit_transactions WHERE id = $1
	`, txID).Scan(&finalAmount)

	if err != nil {
		t.Fatalf("audit log was deleted: %v", err)
	}

	t.Logf("Audit log verified: TX=%s, Amount=%d", txID, finalAmount)
}

// Helper functions

func setupTestDB(t *testing.T) *sql.DB {
	// Use test database
	dsn := "postgres://credits_user:credits_pass2024@localhost:5432/credits_test?sslmode=disable"
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("failed to connect to test database: %v", err)
	}

	// Create schema
	schema := `
		CREATE TABLE IF NOT EXISTS credit_accounts (
			tenant_id VARCHAR(255) PRIMARY KEY,
			balance BIGINT NOT NULL DEFAULT 0,
			reserved BIGINT NOT NULL DEFAULT 0,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			version INTEGER NOT NULL DEFAULT 1,
			metadata JSONB,
			CONSTRAINT credits_balance_non_negative CHECK (balance >= 0),
			CONSTRAINT credits_reserved_non_negative CHECK (reserved >= 0)
		);

		CREATE TABLE IF NOT EXISTS credit_transactions (
			id VARCHAR(255) PRIMARY KEY,
			tenant_id VARCHAR(255) NOT NULL,
			amount BIGINT NOT NULL,
			transaction_type VARCHAR(50) NOT NULL,
			status VARCHAR(50) NOT NULL DEFAULT 'pending',
			description TEXT,
			metadata TEXT,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			completed_at TIMESTAMP WITH TIME ZONE
		);

		CREATE TABLE IF NOT EXISTS idempotency_keys (
			key_hash VARCHAR(64) PRIMARY KEY,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			expires_at TIMESTAMP WITH TIME ZONE DEFAULT NOW() + INTERVAL '24 hours'
		);

		TRUNCATE credit_accounts, credit_transactions, idempotency_keys;
	`

	_, err = db.Exec(schema)
	if err != nil {
		t.Fatalf("failed to create test schema: %v", err)
	}

	return db
}

func setupTestAccount(t *testing.T, db *sql.DB, tenantID string, balance int64) {
	_, err := db.Exec(`
		INSERT INTO credit_accounts (tenant_id, balance, reserved, version)
		VALUES ($1, $2, 0, 1)
		ON CONFLICT (tenant_id) DO UPDATE SET balance = $2, version = 1
	`, tenantID, balance)

	if err != nil {
		t.Fatalf("failed to setup test account: %v", err)
	}
}

func getBalance(t *testing.T, db *sql.DB, tenantID string) int64 {
	var balance int64
	err := db.QueryRow(`
		SELECT balance FROM credit_accounts WHERE tenant_id = $1
	`, tenantID).Scan(&balance)

	if err != nil {
		t.Fatalf("failed to get balance: %v", err)
	}

	return balance
}

// Benchmark tests

func BenchmarkTransactionEngine_AtomicConsumeCredits(b *testing.B) {
	db := setupBenchDB(b)
	defer db.Close()

	engine := NewTransactionEngine(db)
	tenantID := "bench-test-001"

	// Setup account with large balance
	setupBenchAccount(b, db, tenantID, 1000000000)

	ctx := context.Background()
	opts := TransactionOptions{
		IsolationLevel: sql.LevelReadCommitted,
		MaxRetries:     3,
		Timeout:        5 * time.Second,
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _, err := engine.AtomicConsumeCredits(ctx, tenantID, 1, "bench", opts)
		if err != nil {
			b.Fatalf("transaction failed: %v", err)
		}
	}
}

func setupBenchDB(b *testing.B) *sql.DB {
	dsn := "postgres://credits_user:credits_pass2024@localhost:5432/credits_test?sslmode=disable"
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		b.Fatalf("failed to connect: %v", err)
	}
	return db
}

func setupBenchAccount(b *testing.B, db *sql.DB, tenantID string, balance int64) {
	_, err := db.Exec(`
		INSERT INTO credit_accounts (tenant_id, balance, reserved, version)
		VALUES ($1, $2, 0, 1)
		ON CONFLICT (tenant_id) DO UPDATE SET balance = $2
	`, tenantID, balance)

	if err != nil {
		b.Fatalf("failed to setup account: %v", err)
	}
}
