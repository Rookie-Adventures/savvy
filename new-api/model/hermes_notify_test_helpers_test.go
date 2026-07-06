package model

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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

// managerStubInstance mirrors service.HermesInstance (subset).
type managerStubInstance struct {
	InstanceID string `json:"instance_id"`
	UserID     string `json:"user_id"`
	Status     string `json:"status"`
	Plan       string `json:"plan"`
}

// managerStubResources mirrors service.PlanResourceSpec.
type managerStubResources struct {
	CPUQuota  int
	MemLimit  string
	PidsLimit int
}

var managerStubPlanResources = map[string]managerStubResources{
	"default": {CPUQuota: 50000, MemLimit: "768m", PidsLimit: 128},
	"starter": {CPUQuota: 200000, MemLimit: "2g", PidsLimit: 512},
	"pro":     {CPUQuota: 400000, MemLimit: "8g", PidsLimit: 1024},
}

var managerStubGroupToPlanName = map[string]string{
	"default": "FREE",
	"starter": "STARTER",
	"pro":     "PRO",
}

func managerPlanResources(group string) (managerStubResources, bool) {
	r, ok := managerStubPlanResources[group]
	return r, ok
}

func managerGroupToPlanName(group string) (string, bool) {
	n, ok := managerStubGroupToPlanName[group]
	return n, ok
}

func managerURL() string {
	u := os.Getenv("HERMES_MANAGER_URL")
	if u == "" {
		u = "http://localhost:8000"
	}
	return strings.TrimRight(u, "/")
}

func managerSecret() string { return os.Getenv("SAVVY_HMAC_SECRET") }

// managerSign mirrors service.signAndDo (HMAC-SHA256 over
// method\npath\nsha256(body)\ntimestamp\nnonce).
func managerSign(req *http.Request, userID int, body []byte) (*http.Response, error) {
	secret := managerSecret()
	if secret == "" {
		return nil, fmt.Errorf("SAVVY_HMAC_SECRET unset")
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := uuid.New().String()
	if body == nil {
		body = []byte{}
	}
	h := sha256.Sum256(body)
	msg := fmt.Sprintf("%s\n%s\n%s\n%s\n%s", req.Method, req.URL.Path, hex.EncodeToString(h[:]), ts, nonce)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(msg))
	sig := hex.EncodeToString(mac.Sum(nil))
	req.Header.Set("X-Savvy-Timestamp", ts)
	req.Header.Set("X-Savvy-Nonce", nonce)
	req.Header.Set("X-Savvy-Signature", sig)
	req.Header.Set("X-Savvy-User-Id", strconv.Itoa(userID))
	return (&http.Client{Timeout: 5 * time.Second}).Do(req)
}

func managerDecode(resp *http.Response) error {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var env struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := common.Unmarshal(body, &env); err != nil {
		return err
	}
	if !env.Success {
		return fmt.Errorf("manager: %s", env.Message)
	}
	return nil
}

func managerGetRunningInstance(userID int) (*managerStubInstance, error) {
	url := fmt.Sprintf("%s/internal/users/%d/instance", managerURL(), userID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := managerSign(req, userID, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var env struct {
		Success bool                `json:"success"`
		Data    managerStubInstance `json:"data"`
	}
	if err := common.Unmarshal(body, &env); err != nil {
		return nil, err
	}
	if !env.Success {
		return nil, fmt.Errorf("manager: instance not found")
	}
	return &env.Data, nil
}

func managerPostUpgrade(userID int, instanceID, plan string, res managerStubResources) error {
	body := map[string]any{
		"plan":       plan,
		"cpu_quota":  res.CPUQuota,
		"mem_limit":  res.MemLimit,
		"pids_limit": res.PidsLimit,
	}
	b, err := common.Marshal(body)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s/internal/instances/%s/upgrade", managerURL(), instanceID)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := managerSign(req, userID, b)
	if err != nil {
		return err
	}
	return managerDecode(resp)
}

func managerPostDowngrade(userID int, instanceID string, expiresAt time.Time) error {
	body := map[string]any{
		"plan":       "FREE",
		"expires_at": expiresAt.UTC().Format(time.RFC3339),
	}
	b, err := common.Marshal(body)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s/internal/instances/%s/downgrade", managerURL(), instanceID)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := managerSign(req, userID, b)
	if err != nil {
		return err
	}
	return managerDecode(resp)
}
