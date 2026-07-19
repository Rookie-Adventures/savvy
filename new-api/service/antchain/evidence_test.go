package antchain

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
)

// TestSubmitEvidence_NoClient verifies SubmitEvidence returns an error when
// the RestClient is not initialized (ANTCHAIN_ENABLED=false default).
func TestSubmitEvidence_NoClient(t *testing.T) {
	err := SubmitEvidence(model.SubmitOrderEvidenceInput{
		TradeNo: "test-001",
	})
	if err == nil {
		t.Fatal("expected error when restClient is nil")
	}
}
