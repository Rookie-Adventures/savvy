package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

type HermesInstance struct {
	ID              string  `json:"id"`
	Status          string  `json:"status"`
	Plan            string  `json:"plan"`
	RemainingMinutes *int   `json:"remaining_minutes,omitempty"`
	LastError       string  `json:"last_error,omitempty"`
	AccessURL       string  `json:"access_url,omitempty"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

type HermesManagerResponse struct {
	Success bool          `json:"success"`
	Message string        `json:"message"`
	Data    *HermesInstance `json:"data,omitempty"`
}

func getHermesManagerURL() string {
	url := os.Getenv("HERMES_MANAGER_URL")
	if url == "" {
		url = "http://localhost:8000"
	}
	return url
}

func getHermesManagerClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

func GetHermesInstance(userID int) (*HermesInstance, error) {
	url := fmt.Sprintf("%s/internal/users/%d/instance", getHermesManagerURL(), userID)

	resp, err := getHermesManagerClient().Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to hermes-manager: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var managerResp HermesManagerResponse
	if err := json.Unmarshal(body, &managerResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if !managerResp.Success {
		return nil, fmt.Errorf("%s", managerResp.Message)
	}

	return managerResp.Data, nil
}

func StartHermesInstance(userID int, instanceID string) error {
	url := fmt.Sprintf("%s/internal/instances/%s/start", getHermesManagerURL(), instanceID)

	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", fmt.Sprintf("%d", userID))

	resp, err := getHermesManagerClient().Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to hermes-manager: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	var managerResp HermesManagerResponse
	if err := json.Unmarshal(body, &managerResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if !managerResp.Success {
		return fmt.Errorf("%s", managerResp.Message)
	}

	return nil
}

func SleepHermesInstance(userID int, instanceID string) error {
	url := fmt.Sprintf("%s/internal/instances/%s/sleep", getHermesManagerURL(), instanceID)

	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", fmt.Sprintf("%d", userID))

	resp, err := getHermesManagerClient().Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to hermes-manager: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	var managerResp HermesManagerResponse
	if err := json.Unmarshal(body, &managerResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if !managerResp.Success {
		return fmt.Errorf("%s", managerResp.Message)
	}

	return nil
}
