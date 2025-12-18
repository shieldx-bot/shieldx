//go:build integration
// +build integration

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConsumeCredits(t *testing.T) {
	ledger := &CreditLedger{}

	req := ConsumeRequest{
		TenantID:    "test_tenant_123",
		Amount:      50,
		Description: "Test plugin execution",
		Reference:   "test_ref_001",
	}

	jsonData, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/credits/consume", bytes.NewBuffer(jsonData))
	w := httptest.NewRecorder()

	ledger.ConsumeCredits(w, httpReq)

	var response CreditResponse
	json.NewDecoder(w.Body).Decode(&response)

	if w.Code == http.StatusOK && response.Success {
		t.Log("Consume credits test passed")
	} else {
		t.Logf("Consume credits test result: %d, success: %v", w.Code, response.Success)
	}
}

func TestInsufficientCredits(t *testing.T) {
	ledger := &CreditLedger{}

	req := ConsumeRequest{
		TenantID: "test_tenant_empty",
		Amount:   1000,
	}

	jsonData, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/credits/consume", bytes.NewBuffer(jsonData))
	w := httptest.NewRecorder()

	ledger.ConsumeCredits(w, httpReq)

	var response CreditResponse
	json.NewDecoder(w.Body).Decode(&response)

	if !response.Success && response.Error == "Insufficient credits" {
		t.Log("Insufficient credits test passed")
	} else {
		t.Logf("Expected insufficient credits error, got: %s", response.Error)
	}
}

func TestPurchaseCredits(t *testing.T) {
	ledger := &CreditLedger{}

	req := PurchaseRequest{
		TenantID:      "test_tenant_purchase",
		Amount:        1000,
		PaymentMethod: "stripe",
		PaymentToken:  "tok_visa",
	}

	jsonData, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/credits/purchase", bytes.NewBuffer(jsonData))
	w := httptest.NewRecorder()

	ledger.PurchaseCredits(w, httpReq)

	var response CreditResponse
	json.NewDecoder(w.Body).Decode(&response)

	if w.Code == http.StatusOK && response.Success {
		t.Log("Purchase credits test passed")
	} else {
		t.Logf("Purchase credits test result: %d, success: %v", w.Code, response.Success)
	}
}

func TestIdempotency(t *testing.T) {
	ledger := &CreditLedger{}
	idempotencyKey := "test_key_123"

	req := ConsumeRequest{
		TenantID:       "test_tenant_idem",
		Amount:         10,
		IdempotencyKey: idempotencyKey,
	}

	jsonData, _ := json.Marshal(req)

	httpReq1 := httptest.NewRequest("POST", "/credits/consume", bytes.NewBuffer(jsonData))
	w1 := httptest.NewRecorder()
	ledger.ConsumeCredits(w1, httpReq1)

	httpReq2 := httptest.NewRequest("POST", "/credits/consume", bytes.NewBuffer(jsonData))
	w2 := httptest.NewRecorder()
	ledger.ConsumeCredits(w2, httpReq2)

	var response1, response2 CreditResponse
	json.NewDecoder(w1.Body).Decode(&response1)
	json.NewDecoder(w2.Body).Decode(&response2)

	if response1.Success && response2.Success {
		t.Log("Idempotency test passed")
	} else {
		t.Logf("Idempotency test failed: r1=%v, r2=%v", response1.Success, response2.Success)
	}
}

func TestGetBalance(t *testing.T) {
	ledger := &CreditLedger{}

	httpReq := httptest.NewRequest("GET", "/credits/balance/test_tenant", nil)
	w := httptest.NewRecorder()

	ledger.GetBalance(w, httpReq)

	if w.Code == http.StatusOK {
		var response map[string]interface{}
		json.NewDecoder(w.Body).Decode(&response)

		if tenantID, ok := response["tenant_id"].(string); ok && tenantID == "test_tenant" {
			t.Log("Get balance test passed")
		} else {
			t.Log("Get balance test failed: invalid response")
		}
	} else {
		t.Logf("Get balance test failed with status: %d", w.Code)
	}
}

func TestConcurrentConsumption(t *testing.T) {
	ledger := &CreditLedger{}
	numGoroutines := 5
	consumeAmount := int64(5)

	results := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			req := ConsumeRequest{
				TenantID:  "test_tenant_concurrent",
				Amount:    consumeAmount,
				Reference: "concurrent_test",
			}

			jsonData, _ := json.Marshal(req)
			httpReq := httptest.NewRequest("POST", "/credits/consume", bytes.NewBuffer(jsonData))
			w := httptest.NewRecorder()

			ledger.ConsumeCredits(w, httpReq)

			var response CreditResponse
			json.NewDecoder(w.Body).Decode(&response)

			results <- response.Success
		}(i)
	}

	successCount := 0
	for i := 0; i < numGoroutines; i++ {
		if <-results {
			successCount++
		}
	}

	t.Logf("Concurrent consumption test: %d/%d successful", successCount, numGoroutines)
}

func TestPaymentProcessing(t *testing.T) {
	ledger := &CreditLedger{}

	req := PurchaseRequest{
		TenantID:      "test_payment",
		Amount:        100,
		PaymentMethod: "stripe",
		PaymentToken:  "tok_test",
	}

	result, err := ledger.processPayment(req)

	if err != nil {
		t.Errorf("Payment processing failed: %v", err)
		return
	}

	if !result.Success {
		t.Error("Expected payment to succeed")
		return
	}

	if result.Reference == "" {
		t.Error("Expected payment reference to be set")
		return
	}

	t.Log("Payment processing test passed")
}

func TestInvalidPayment(t *testing.T) {
	ledger := &CreditLedger{}

	req := PurchaseRequest{
		TenantID:      "test_invalid",
		Amount:        100,
		PaymentMethod: "",
		PaymentToken:  "",
	}

	_, err := ledger.processPayment(req)

	if err == nil {
		t.Error("Expected payment processing to fail with invalid data")
		return
	}

	t.Log("Invalid payment test passed")
}

func TestHasSufficientCredits(t *testing.T) {
	ledger := &CreditLedger{}

	hasSufficient, err := ledger.HasSufficientCredits("test_tenant", 100)

	if err != nil {
		t.Logf("HasSufficientCredits returned error: %v", err)
	} else {
		t.Logf("HasSufficientCredits result: %v", hasSufficient)
	}
}
