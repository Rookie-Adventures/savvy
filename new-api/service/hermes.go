package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/google/uuid"
)

// HermesInstance mirrors the manager's InstanceResponse (snake_case).
// The frontend adapter normalizes status enum to lowercase and computes
// remaining_minutes / access_url on the controller side as needed.
type HermesInstance struct {
	InstanceID    string `json:"instance_id"`
	UserID        string `json:"user_id"`
	Status        string `json:"status"` // manager returns UPPERCASE enum: RUNNING / SLEEPING / ...
	Plan          string `json:"plan"`   // FREE / PAID_RESIDENT / ...
	ContainerName string `json:"container_name,omitempty"`
	VolumeName    string `json:"volume_name,omitempty"`
	StartedAt     string `json:"started_at,omitempty"`
	ExpiresAt     string `json:"expires_at,omitempty"`
}

// HermesAccessToken mirrors manager's AccessTokenResponse.
type HermesAccessToken struct {
	Token        string `json:"token"`
	ExpiresAt    string `json:"expires_at"`
	WorkspaceURL string `json:"workspace_url"`
}

// HermesUpsertResult mirrors manager's UserUpsertResponse.
type HermesUpsertResult struct {
	UserID  string `json:"user_id"`
	Created bool   `json:"created"`
}

type HermesManagerResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func getHermesManagerURL() string {
	url := os.Getenv("HERMES_MANAGER_URL")
	if url == "" {
		url = "http://localhost:8000"
	}
	return strings.TrimRight(url, "/")
}

func getHermesHmacSecret() string {
	return os.Getenv("SAVVY_HMAC_SECRET")
}

func getHermesManagerClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

// signAndDo injects HMAC-SHA256 signature headers required by manager's
// require_hmac dependency and sends the request. bodyBytes must be the exact
// bytes that will be sent as the body (nil/empty for GET).
//
// Signature string MUST match manager (auth.py):
//
//	f"{method}\n{path}\n{sha256(body).hexdigest()}\n{timestamp}\n{nonce}"
//
// where path is the URL path WITHOUT query string.
func signAndDo(req *http.Request, userID int, bodyBytes []byte) (*http.Response, error) {
	secret := getHermesHmacSecret()
	if secret == "" {
		return nil, fmt.Errorf("SAVVY_HMAC_SECRET is not configured")
	}

	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := uuid.New().String()
	if bodyBytes == nil {
		bodyBytes = []byte{}
	}
	bodyHash := sha256.Sum256(bodyBytes)

	message := fmt.Sprintf("%s\n%s\n%s\n%s\n%s",
		req.Method, req.URL.Path, hex.EncodeToString(bodyHash[:]), timestamp, nonce)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	signature := hex.EncodeToString(mac.Sum(nil))

	req.Header.Set("X-Savvy-Timestamp", timestamp)
	req.Header.Set("X-Savvy-Nonce", nonce)
	req.Header.Set("X-Savvy-Signature", signature)
	req.Header.Set("X-Savvy-User-Id", strconv.Itoa(userID))

	return getHermesManagerClient().Do(req)
}

// decodeManagerResponse reads & unmarshals the manager response envelope.
// Returns the raw Data payload on success, or an error on failure/parse error.
func decodeManagerResponse(resp *http.Response) (json.RawMessage, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read manager response: %w", err)
	}

	var managerResp HermesManagerResponse
	if err := common.Unmarshal(body, &managerResp); err != nil {
		return nil, fmt.Errorf("failed to parse manager response: %w", err)
	}
	if !managerResp.Success {
		msg := managerResp.Message
		if msg == "" {
			msg = "manager returned failure"
		}
		return nil, fmt.Errorf("%s", msg)
	}
	return managerResp.Data, nil
}

// UpsertHermesUser creates or refreshes the user mapping in manager.
// Called on login success / first Hermes page visit.
func UpsertHermesUser(userID int) (*HermesUpsertResult, error) {
	url := fmt.Sprintf("%s/internal/users/upsert", getHermesManagerURL())
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := signAndDo(req, userID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to hermes-manager: %w", err)
	}

	data, err := decodeManagerResponse(resp)
	if err != nil {
		return nil, err
	}
	var result HermesUpsertResult
	if err := common.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse upsert result: %w", err)
	}
	return &result, nil
}

func GetHermesInstance(userID int) (*HermesInstance, error) {
	url := fmt.Sprintf("%s/internal/users/%d/instance", getHermesManagerURL(), userID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := signAndDo(req, userID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to hermes-manager: %w", err)
	}

	data, err := decodeManagerResponse(resp)
	if err != nil {
		return nil, err
	}
	var inst HermesInstance
	if err := common.Unmarshal(data, &inst); err != nil {
		return nil, fmt.Errorf("failed to parse instance: %w", err)
	}
	return &inst, nil
}

// CreateHermesInstance creates a new (or returns existing) workspace instance.
func CreateHermesInstance(userID int) (*HermesInstance, error) {
	url := fmt.Sprintf("%s/internal/users/%d/instance", getHermesManagerURL(), userID)
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := signAndDo(req, userID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to hermes-manager: %w", err)
	}

	data, err := decodeManagerResponse(resp)
	if err != nil {
		return nil, err
	}
	var inst HermesInstance
	if err := common.Unmarshal(data, &inst); err != nil {
		return nil, fmt.Errorf("failed to parse instance: %w", err)
	}
	return &inst, nil
}

func StartHermesInstance(userID int, instanceID string) error {
	url := fmt.Sprintf("%s/internal/instances/%s/start", getHermesManagerURL(), instanceID)
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := signAndDo(req, userID, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to hermes-manager: %w", err)
	}
	_, err = decodeManagerResponse(resp)
	return err
}

func SleepHermesInstance(userID int, instanceID string) error {
	url := fmt.Sprintf("%s/internal/instances/%s/sleep", getHermesManagerURL(), instanceID)
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := signAndDo(req, userID, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to hermes-manager: %w", err)
	}
	_, err = decodeManagerResponse(resp)
	return err
}

// GetHermesAccessToken issues a short-lived workspace access token for an
// already-running instance.
func GetHermesAccessToken(userID int, instanceID string) (*HermesAccessToken, error) {
	url := fmt.Sprintf("%s/internal/instances/%s/access-token", getHermesManagerURL(), instanceID)
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := signAndDo(req, userID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to hermes-manager: %w", err)
	}

	data, err := decodeManagerResponse(resp)
	if err != nil {
		return nil, err
	}
	var token HermesAccessToken
	if err := common.Unmarshal(data, &token); err != nil {
		return nil, fmt.Errorf("failed to parse access token: %w", err)
	}
	return &token, nil
}

// HealthCheckHermesManager is a lightweight GET /health used by the status
// endpoint. The /health route is NOT behind require_hmac, so no signing here.
func HealthCheckHermesManager() bool {
	url := fmt.Sprintf("%s/health", getHermesManagerURL())
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// Note: encoding/json is imported ONLY for the json.RawMessage type reference
// in HermesManagerResponse.Data. All actual (de)serialization goes through
// common.Unmarshal, per new-api/AGENTS.md project rule.
var _ = json.RawMessage(nil)
