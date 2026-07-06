package service

import (
	"bytes"
	"context"
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
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/perf_metrics"
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

// HermesProviderState mirrors manager's GET /provider-state response.
// It deliberately surfaces only source/model/key_set_at — the api_key itself
// is NEVER returned by the manager and is never present here.
type HermesProviderState struct {
	Source   string `json:"source"`     // ours | user | none
	Model    string `json:"model"`      // currently configured model
	KeySetAt string `json:"key_set_at"` // ISO time of last key set
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

// StartHermesInstance starts the workspace instance and forwards the provider
// key (required on first start) to the manager. Empty providerBaseURL/Model
// let the manager apply its defaults; providerAPIKey is forwarded as-is.
//
// The wire shape to manager is snake_case JSON:
//
//	{"provider_api_key": ..., "provider_base_url"?: ..., "provider_model"?: ...}
func StartHermesInstance(userID int, instanceID, providerAPIKey, providerBaseURL, providerModel string) error {
	body := map[string]any{
		"provider_api_key": providerAPIKey,
	}
	if providerBaseURL != "" {
		body["provider_base_url"] = providerBaseURL
	}
	if providerModel != "" {
		body["provider_model"] = providerModel
	}
	bodyBytes, err := common.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal start body: %w", err)
	}

	url := fmt.Sprintf("%s/internal/instances/%s/start", getHermesManagerURL(), instanceID)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := signAndDo(req, userID, bodyBytes)
	if err != nil {
		return fmt.Errorf("failed to connect to hermes-manager: %w", err)
	}
	_, err = decodeManagerResponse(resp)
	return err
}

// RevokeHermesProviderKey clears the LLM provider key snapshot (DB + container)
// at the manager. Sends no body, expects the manager to revoke and respond
// with a success envelope.
func RevokeHermesProviderKey(userID int, instanceID string) error {
	url := fmt.Sprintf("%s/internal/instances/%s/revoke-provider-key", getHermesManagerURL(), instanceID)
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

// GetHermesProviderState returns the current provider source/model/key-set
// timestamp for the instance. The api_key itself is never surfaced by the
// manager and is never present in HermesProviderState.
func GetHermesProviderState(userID int, instanceID string) (*HermesProviderState, error) {
	url := fmt.Sprintf("%s/internal/instances/%s/provider-state", getHermesManagerURL(), instanceID)
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
	var state HermesProviderState
	if err := common.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse provider state: %w", err)
	}
	return &state, nil
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

// CallHermesAgentStream makes a stream POST to hermes-agent gateway (FastAPI) at HERMES_AGENT_URL.
// It parses the OpenAI-compatible EventSource SSE lines, forwards raw JSON payloads (excluding the "data: " prefix and [DONE]),
// and measures performance metrics (TTFT, latency, tokens), calling perfmetrics.RecordHermesSample.
func CallHermesAgentStream(ctx context.Context, group string, message string, agentType string, outChan chan<- []byte, errChan chan<- error) {
	startTime := time.Now()
	var firstResponseTime time.Time
	var ttftMs int64
	var tokenCount int64
	success := false

	defer func() {
		latencyMs := time.Since(startTime).Milliseconds()
		// Record telemetry
		perfmetrics.RecordHermesSample(agentType, group, success, ttftMs, latencyMs, tokenCount)
		common.SysLog(fmt.Sprintf("[hermes-agent-telemetry] success=%t type=%s group=%s latency=%dms ttft=%dms tokens=%d", success, agentType, group, latencyMs, ttftMs, tokenCount))
	}()

	agentURL := os.Getenv("HERMES_AGENT_URL")
	if agentURL == "" {
		agentURL = "http://127.0.0.1:8642"
	}
	agentURL = strings.TrimRight(agentURL, "/") + "/v1/chat/completions"

	// Construct OpenAI compatible payload
	payload := map[string]any{
		"model": "hermes-agent",
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": message,
			},
		},
		"stream": true,
	}

	jsonData, err := common.Marshal(payload)
	if err != nil {
		errChan <- fmt.Errorf("failed to marshal request payload: %w", err)
		return
	}

	req, err := http.NewRequestWithContext(ctx, "POST", agentURL, strings.NewReader(string(jsonData)))
	if err != nil {
		errChan <- fmt.Errorf("failed to create request: %w", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	// Use custom client
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		errChan <- fmt.Errorf("failed to connect to hermes-agent: %w", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		errChan <- fmt.Errorf("hermes-agent returned status %d: %s", resp.StatusCode, string(body))
		return
	}

	// Read stream line by line
	// bufio.Reader handles large lines
	bufReader := io.Reader(resp.Body)
	// We read line by line manually or using a scanner. bufio.Reader is safe.
	lineReader := strings.NewReader("")
	_ = lineReader

	// Let's use bufReader but wrap in a scanner
	// Note: OpenAI chunk stream lines look like:
	// data: {"id":"chatcmpl-...","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"..."},"finish_reason":null}]}
	// data: [DONE]
	// There can also be comments (starting with ':')
	r := io.LimitReader(bufReader, 1024*1024*50) // Safeguard limit 50MB
	dec := strings.Builder{}
	_ = dec

	var buf [4096]byte
	var pending []byte

	for {
		select {
		case <-ctx.Done():
			errChan <- ctx.Err()
			return
		default:
			n, err := r.Read(buf[:])
			if n > 0 {
				chunk := append(pending, buf[:n]...)
				lines := strings.Split(string(chunk), "\n")
				if len(lines) > 0 {
					pending = []byte(lines[len(lines)-1])
					for i := 0; i < len(lines)-1; i++ {
						line := strings.TrimSpace(lines[i])
						if line == "" {
							continue
						}
						if strings.HasPrefix(line, ":") {
							continue // SSE comment
						}
						if strings.HasPrefix(line, "data: ") {
							data := strings.TrimPrefix(line, "data: ")
							data = strings.TrimSpace(data)
							if data == "[DONE]" {
								continue
							}

							// Record TTFT
							if firstResponseTime.IsZero() {
								firstResponseTime = time.Now()
								ttftMs = firstResponseTime.Sub(startTime).Milliseconds()
							}

							// Increment estimated token count or actual tokens
							// Since this is a chunk, we count each chunk as 1 token, or count words/chars.
							// Counting delta contents is highly accurate.
							var chunkObj struct {
								Choices []struct {
									Delta struct {
										Content string `json:"content"`
									} `json:"delta"`
								} `json:"choices"`
							}
							if err := common.Unmarshal([]byte(data), &chunkObj); err == nil {
								if len(chunkObj.Choices) > 0 && chunkObj.Choices[0].Delta.Content != "" {
									// Standard estimation: 1 token ~= 4 characters in English, or 1 character in Chinese.
									// To be accurate, we can count chunks or estimate:
									tokenCount++
								}
							} else {
								tokenCount++ // fallback increment
							}

							outChan <- []byte(data)
						}
					}
				}
			}
			if err != nil {
				if err == io.EOF {
					if len(pending) > 0 {
						line := strings.TrimSpace(string(pending))
						if strings.HasPrefix(line, "data: ") {
							data := strings.TrimPrefix(line, "data: ")
							data = strings.TrimSpace(data)
							if data != "[DONE]" {
								outChan <- []byte(data)
							}
						}
					}
					success = true
					return
				}
				errChan <- fmt.Errorf("error reading stream: %w", err)
				return
			}
		}
	}
}

// PlanResourceSpec mirrors manager's PLAN_RESOURCES per-tier CPU/RAM/PIDs.
type PlanResourceSpec struct {
	CPUQuota   int
	MemLimit   string
	PidsLimit  int
}

// PlanResources mirrors savvy-manager/app/docker_manager.py PLAN_RESOURCES.
// Keyed by user group (default/starter/pro), matching SubscriptionPlan.UpgradeGroup.
var PlanResources = map[string]PlanResourceSpec{
	"default": {CPUQuota: 50000, MemLimit: "768m", PidsLimit: 128},
	"starter": {CPUQuota: 200000, MemLimit: "2g", PidsLimit: 512},
	"pro":     {CPUQuota: 400000, MemLimit: "8g", PidsLimit: 1024},
}

// groupToPlanName maps a user group to manager's PlanType string.
var groupToPlanName = map[string]string{
	"default": "FREE",
	"starter": "STARTER",
	"pro":     "PRO",
}

// GroupToPlanName maps a user group to the manager's PlanType string.
// Exported wrapper over groupToPlanName so model can call into service without
// importing the map directly (avoids leaking internals across the package line).
func GroupToPlanName(group string) (string, bool) {
	name, ok := groupToPlanName[group]
	return name, ok
}

// NotifyManagerUpgrade finds the user's running instance and asks the manager
// to hot-upgrade its container resources. Called from model after a
// subscription order commits. Network failure does NOT roll back the order;
// the manager scanner is the safety net.
func NotifyManagerUpgrade(userID int, upgradeGroup string) error {
	inst, err := GetHermesInstance(userID)
	if err != nil {
		return err
	}
	if inst == nil || inst.Status != "RUNNING" {
		return nil // no running instance to upgrade; user.group already elevated
	}
	res, ok := PlanResources[upgradeGroup]
	if !ok {
		return nil // unknown group, nothing to send
	}
	planName, ok := GroupToPlanName(upgradeGroup)
	if !ok {
		return nil
	}
	return UpgradeHermesInstance(userID, inst.InstanceID, planName, res.CPUQuota, res.MemLimit, res.PidsLimit)
}

// NotifyManagerDowngrade finds the user's running instance and asks the manager
// to downgrade to FREE with a 2h free window. Called from model after a
// subscription expiry commits.
func NotifyManagerDowngrade(userID int) error {
	inst, err := GetHermesInstance(userID)
	if err != nil {
		return err
	}
	if inst == nil || inst.Status != "RUNNING" {
		return nil
	}
	return DowngradeHermesInstance(userID, inst.InstanceID, time.Now().Add(2*time.Hour))
}

func init() {
	// Wire the model-layer notify hooks to the real service impls. service
	// already imports model, so this assignment lives here to avoid a model→
	// service import cycle. Tests override the model vars directly.
	model.NotifyManagerUpgradeFn = NotifyManagerUpgrade
	model.NotifyManagerDowngradeFn = NotifyManagerDowngrade
}

// UpgradeHermesInstance 通知 manager 升级在跑容器资源 + 改 plan + 清免费窗。
// plan 是 manager PlanType 字符串("STARTER"/"PRO");资源数值由 new-api 传入,manager 不反查。
func UpgradeHermesInstance(userID int, instanceID, plan string, cpuQuota int, memLimit string, pidsLimit int) error {
	body := map[string]any{
		"plan":        plan,
		"cpu_quota":   cpuQuota,
		"mem_limit":   memLimit,
		"pids_limit":  pidsLimit,
	}
	bodyBytes, err := common.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal upgrade body: %w", err)
	}

	url := fmt.Sprintf("%s/internal/instances/%s/upgrade", getHermesManagerURL(), instanceID)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := signAndDo(req, userID, bodyBytes)
	if err != nil {
		return fmt.Errorf("failed to connect to hermes-manager: %w", err)
	}
	_, err = decodeManagerResponse(resp)
	return err
}

// DowngradeHermesInstance 通知 manager 降级:改 plan=FREE + 设免费 2h 窗。不动运行容器。
func DowngradeHermesInstance(userID int, instanceID string, expiresAt time.Time) error {
	body := map[string]any{
		"plan":       "FREE",
		"expires_at": expiresAt.UTC().Format(time.RFC3339),
	}
	bodyBytes, err := common.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal downgrade body: %w", err)
	}

	url := fmt.Sprintf("%s/internal/instances/%s/downgrade", getHermesManagerURL(), instanceID)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := signAndDo(req, userID, bodyBytes)
	if err != nil {
		return fmt.Errorf("failed to connect to hermes-manager: %w", err)
	}
	_, err = decodeManagerResponse(resp)
	return err
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
