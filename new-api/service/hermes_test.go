package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestGetHermesManagerURL(t *testing.T) {
	// Test default URL
	url := getHermesManagerURL()
	if url != "http://localhost:8000" {
		t.Errorf("expected default URL, got %s", url)
	}

	// Test with environment variable
	os.Setenv("HERMES_MANAGER_URL", "http://test:9000")
	defer os.Unsetenv("HERMES_MANAGER_URL")

	url = getHermesManagerURL()
	if url != "http://test:9000" {
		t.Errorf("expected env URL, got %s", url)
	}
}

func TestGetHermesInstance(t *testing.T) {
	// Mock hermes-manager server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/users/123/instance" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		resp := HermesManagerResponse{
			Success: true,
			Message: "",
			Data: &HermesInstance{
				ID:     "test-instance",
				Status: "running",
				Plan:   "free",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	os.Setenv("HERMES_MANAGER_URL", server.URL)
	defer os.Unsetenv("HERMES_MANAGER_URL")

	instance, err := GetHermesInstance(123)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if instance.ID != "test-instance" {
		t.Errorf("expected instance ID test-instance, got %s", instance.ID)
	}

	if instance.Status != "running" {
		t.Errorf("expected status running, got %s", instance.Status)
	}
}

func TestStartHermesInstance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/instances/test-instance/start" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}

		resp := HermesManagerResponse{
			Success: true,
			Message: "",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	os.Setenv("HERMES_MANAGER_URL", server.URL)
	defer os.Unsetenv("HERMES_MANAGER_URL")

	err := StartHermesInstance(123, "test-instance")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSleepHermesInstance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/instances/test-instance/sleep" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}

		resp := HermesManagerResponse{
			Success: true,
			Message: "",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	os.Setenv("HERMES_MANAGER_URL", server.URL)
	defer os.Unsetenv("HERMES_MANAGER_URL")

	err := SleepHermesInstance(123, "test-instance")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
