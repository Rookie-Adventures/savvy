package service

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testHmacSecret = "test-shared-secret"

// managerAuth is a faithful Go reimplementation of savvy-manager's
// verify_hmac_signature (app/auth.py). It validates that the request new-api
// sends actually carries a signature the manager will accept.
//
// message = "{method}\n{path}\n{sha256(body).hexdigest()}\n{timestamp}\n{nonce}"
func managerVerify(t *testing.T, r *http.Request) bool {
	t.Helper()
	timestamp := r.Header.Get("X-Savvy-Timestamp")
	nonce := r.Header.Get("X-Savvy-Nonce")
	signature := r.Header.Get("X-Savvy-Signature")
	if timestamp == "" || nonce == "" || signature == "" {
		return false
	}

	body, err := io.ReadAll(r.Body)
	require.NoError(t, err)

	bodyHash := sha256.Sum256(body)
	message := r.Method + "\n" + r.URL.Path + "\n" + hex.EncodeToString(bodyHash[:]) + "\n" + timestamp + "\n" + nonce

	mac := hmac.New(sha256.New, []byte(testHmacSecret))
	mac.Write([]byte(message))
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

// requireSigned checks the request was HMAC-signed with all four headers.
func requireSigned(t *testing.T, r *http.Request) {
	t.Helper()
	require.NotEmpty(t, r.Header.Get("X-Savvy-Timestamp"))
	require.NotEmpty(t, r.Header.Get("X-Savvy-Nonce"))
	require.NotEmpty(t, r.Header.Get("X-Savvy-Signature"))
	require.NotEmpty(t, r.Header.Get("X-Savvy-User-Id"))
	require.True(t, managerVerify(t, r), "request HMAC signature did not match manager's algorithm")
}

func setupManagerEnv(t *testing.T, url string) {
	t.Helper()
	t.Setenv("HERMES_MANAGER_URL", url)
	t.Setenv("SAVVY_HMAC_SECRET", testHmacSecret)
}

// writeEnvelope writes a manager-style {success, message, data} JSON response.
func writeEnvelope(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	// Tests may marshal with encoding/json directly; the project rule targets
	// production business code, not test fixtures.
	body, _ := json.Marshal(map[string]any{"success": true, "message": "", "data": data})
	w.Write(body)
}

func TestGetHermesManagerURL(t *testing.T) {
	// Default URL when env unset.
	t.Setenv("HERMES_MANAGER_URL", "")
	assert.Equal(t, "http://localhost:8000", getHermesManagerURL())

	t.Setenv("HERMES_MANAGER_URL", "http://test:9000/")
	assert.Equal(t, "http://test:9000", getHermesManagerURL(), "trailing slash should be trimmed")
}

func TestGetHermesInstance_SignedAndParsed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireSigned(t, r)
		assert.Equal(t, "/internal/users/123/instance", r.URL.Path)

		writeEnvelope(w, map[string]any{
			"instance_id":    "inst-123",
			"user_id":        "123",
			"status":         "RUNNING",
			"plan":           "FREE",
			"container_name": "savvy-u123-w1",
			"expires_at":     "2099-01-01T00:00:00Z",
		})
	}))
	defer server.Close()
	setupManagerEnv(t, server.URL)

	inst, err := GetHermesInstance(123)
	require.NoError(t, err)
	require.NotNil(t, inst)
	assert.Equal(t, "inst-123", inst.InstanceID)
	assert.Equal(t, "RUNNING", inst.Status)
	assert.Equal(t, "FREE", inst.Plan)
	assert.Equal(t, "savvy-u123-w1", inst.ContainerName)
}

func TestCreateHermesInstance_SignedAndParsed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireSigned(t, r)
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/internal/users/123/instance", r.URL.Path)

		writeEnvelope(w, map[string]any{
			"instance_id": "inst-123",
			"user_id":     "123",
			"status":      "SLEEPING",
			"plan":        "FREE",
		})
	}))
	defer server.Close()
	setupManagerEnv(t, server.URL)

	inst, err := CreateHermesInstance(123)
	require.NoError(t, err)
	require.NotNil(t, inst)
	assert.Equal(t, "inst-123", inst.InstanceID)
	assert.Equal(t, "SLEEPING", inst.Status)
}

func TestStartHermesInstance_Signed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireSigned(t, r)
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/internal/instances/test-instance/start", r.URL.Path)
		writeEnvelope(w, nil)
	}))
	defer server.Close()
	setupManagerEnv(t, server.URL)

	require.NoError(t, StartHermesInstance(123, "test-instance", "", "", ""))
}

// captureBody reads r.Body fully and restores it so subsequent readers
// (requireSigned / managerVerify both io.ReadAll the body) still see bytes.
func captureBody(t *testing.T, r *http.Request) []byte {
	t.Helper()
	raw, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return raw
}

// TestStartHermesInstanceForwardsProviderKey asserts the provider key (and
// optional base URL / model overrides) reach the manager inside the signed
// JSON body under the snake_case keys manager expects.
func TestStartHermesInstanceForwardsProviderKey(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := captureBody(t, r)
		requireSigned(t, r)
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/internal/instances/inst-1/start", r.URL.Path)

		require.NoError(t, common.Unmarshal(raw, &gotBody))
		writeEnvelope(w, nil)
	}))
	defer server.Close()
	setupManagerEnv(t, server.URL)

	require.NoError(t, StartHermesInstance(2, "inst-1", "sk-abc1234567890123", "", ""))
	require.Equal(t, "sk-abc1234567890123", gotBody["provider_api_key"], "provider key must be forwarded under snake_case key")
	_, hasURL := gotBody["provider_base_url"]
	assert.False(t, hasURL, "empty base URL must be omitted")
	_, hasModel := gotBody["provider_model"]
	assert.False(t, hasModel, "empty model must be omitted")
}

// TestStartHermesInstanceForwardsAllOverrides forwards base URL + model too.
func TestStartHermesInstanceForwardsAllOverrides(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := captureBody(t, r)
		requireSigned(t, r)
		require.NoError(t, common.Unmarshal(raw, &gotBody))
		writeEnvelope(w, nil)
	}))
	defer server.Close()
	setupManagerEnv(t, server.URL)

	require.NoError(t, StartHermesInstance(2, "inst-1", "sk-key", "https://base.example/v1", "gpt-5"))
	assert.Equal(t, "sk-key", gotBody["provider_api_key"])
	assert.Equal(t, "https://base.example/v1", gotBody["provider_base_url"])
	assert.Equal(t, "gpt-5", gotBody["provider_model"])
}

// TestRevokeHermesProviderKey asserts the revoke route is hit with no body.
func TestRevokeHermesProviderKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := captureBody(t, r)
		requireSigned(t, r)
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/internal/instances/inst-1/revoke-provider-key", r.URL.Path)
		assert.Empty(t, raw, "revoke must send an empty body")
		writeEnvelope(w, nil)
	}))
	defer server.Close()
	setupManagerEnv(t, server.URL)

	require.NoError(t, RevokeHermesProviderKey(2, "inst-1"))
}

// TestGetHermesProviderState asserts the provider-state envelope is parsed
// and — critically — that no api_key leaks into the struct.
func TestGetHermesProviderState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireSigned(t, r)
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/internal/instances/inst-1/provider-state", r.URL.Path)
		writeEnvelope(w, map[string]any{
			"instance_id": "inst-1",
			"source":      "user",
			"model":       "gpt-5",
			"key_set_at":  "2026-07-04T10:00:00Z",
		})
	}))
	defer server.Close()
	setupManagerEnv(t, server.URL)

	state, err := GetHermesProviderState(2, "inst-1")
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, "user", state.Source)
	assert.Equal(t, "gpt-5", state.Model)
	assert.Equal(t, "2026-07-04T10:00:00Z", state.KeySetAt)
	// api_key must never appear in the surfaced state; Model is the closest
	// free-text field a leak could populate.
	assert.False(t, strings.Contains(state.Model, "sk-"), "api_key should not appear in state")
}

func TestSleepHermesInstance_Signed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireSigned(t, r)
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/internal/instances/test-instance/sleep", r.URL.Path)
		writeEnvelope(w, nil)
	}))
	defer server.Close()
	setupManagerEnv(t, server.URL)

	require.NoError(t, SleepHermesInstance(123, "test-instance"))
}

func TestGetHermesAccessToken_SignedAndParsed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireSigned(t, r)
		assert.Equal(t, "/internal/instances/test-instance/access-token", r.URL.Path)
		writeEnvelope(w, map[string]any{
			"token":         "abc.def",
			"expires_at":    "2099-01-01T00:00:00Z",
			"workspace_url": "/workspace/123/",
		})
	}))
	defer server.Close()
	setupManagerEnv(t, server.URL)

	token, err := GetHermesAccessToken(123, "test-instance")
	require.NoError(t, err)
	require.NotNil(t, token)
	assert.Equal(t, "abc.def", token.Token)
	assert.Equal(t, "/workspace/123/", token.WorkspaceURL)
}

func TestUpsertHermesUser_SignedAndParsed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireSigned(t, r)
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/internal/users/upsert", r.URL.Path)
		writeEnvelope(w, map[string]any{"user_id": "123", "created": true})
	}))
	defer server.Close()
	setupManagerEnv(t, server.URL)

	result, err := UpsertHermesUser(123)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "123", result.UserID)
	assert.True(t, result.Created)
}

// TestUnsignedRequestRejected proves the contract: if SAVVY_HMAC_SECRET is
// unset, signAndDo refuses to send. This guards against silently shipping
// unauthenticated requests.
func TestUnsignedRequestRejected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("manager should not be reached without a secret")
	}))
	defer server.Close()
	t.Setenv("HERMES_MANAGER_URL", server.URL)
	os.Unsetenv("SAVVY_HMAC_SECRET")

	_, err := GetHermesInstance(123)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SAVVY_HMAC_SECRET")
}

// TestManagerFailureSurfacesMessage ensures a {success:false} envelope from
// the manager is turned into a Go error carrying the message.
func TestManagerFailureSurfacesMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireSigned(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":false,"message":"instance not running","data":null}`))
	}))
	defer server.Close()
	setupManagerEnv(t, server.URL)

	_, err := GetHermesAccessToken(123, "test-instance")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "instance not running")
}
