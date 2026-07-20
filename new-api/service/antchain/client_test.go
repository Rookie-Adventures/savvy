//go:build manual

package antchain

import (
	"os"
	"testing"
)

// TestShake verifies the antchain RestClient can perform the B-class auth
// handshake against the real RestUrl. Requires valid access.key and env vars.
// Run: go test -run TestShake -tags=manual ./service/antchain/
func TestShake(t *testing.T) {
	os.Setenv("ANTCHAIN_ENABLED", "true")
	// These must be set in the environment before running:
	//   ANTCHAIN_ACCESS_ID, ANTCHAIN_ACCESS_SECRET_FILE, ANTCHAIN_REST_URL
	Init()
	if !Enabled {
		t.Fatal("antchain Init failed; check env vars and access.key path")
	}
	if restClient == nil {
		t.Fatal("restClient is nil after successful Init")
	}
	if restClient.RestToken == "" {
		t.Fatal("RestToken is empty; shake did not succeed")
	}
	t.Logf("shake OK, token prefix: %s...", restClient.RestToken[:min(20, len(restClient.RestToken))])
}
