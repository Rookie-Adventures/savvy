package controller

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// hermesInstanceVO is the view object returned to the frontend.
// Field names (camelCase via json tags) match the HermesInstance interface
// in web/default/src/features/hermes/types.ts.
type hermesInstanceVO struct {
	ID              string `json:"id"`
	Status          string `json:"status"` // lowercase: running / sleeping / creating / error
	Plan            string `json:"plan"`
	RemainingMinutes *int  `json:"remainingMinutes,omitempty"`
	AccessURL       string `json:"accessUrl,omitempty"`
	CreatedAt       string `json:"createdAt,omitempty"`
	UpdatedAt       string `json:"updatedAt,omitempty"`
}

// toVO converts the manager's raw instance into the frontend view object.
// Manager returns UPPERCASE status enums (RUNNING/SLEEPING/...); the frontend
// expects lowercase. remainingMinutes is derived from expires_at.
func toVO(inst *service.HermesInstance) hermesInstanceVO {
	vo := hermesInstanceVO{
		ID:     inst.InstanceID,
		Status: normalizeStatus(inst.Status),
		Plan:   inst.Plan,
	}
	if inst.ExpiresAt != "" {
		if mins := remainingMinutes(inst.ExpiresAt); mins != nil {
			vo.RemainingMinutes = mins
		}
	}
	if inst.StartedAt != "" {
		vo.CreatedAt = inst.StartedAt
	}
	return vo
}

func normalizeStatus(s string) string {
	low := strings.ToLower(s)
	switch low {
	case "running", "sleeping", "creating", "error", "not_created", "deleting":
		// map manager-only states to the four the frontend knows about
		if low == "not_created" {
			return "creating"
		}
		if low == "deleting" {
			return "error"
		}
		return low
	default:
		return "error"
	}
}

func remainingMinutes(expiresAtISO string) *int {
	t, err := time.Parse(time.RFC3339, expiresAtISO)
	if err != nil {
		return nil
	}
	mins := int(time.Until(t).Minutes())
	if mins < 0 {
		mins = 0
	}
	return &mins
}

func getUserID(c *gin.Context) (int, bool) {
	userID := c.GetInt("id")
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "unauthorized",
		})
		return 0, false
	}
	return userID, true
}

func GetHermesInstance(c *gin.Context) {
	userID, ok := getUserID(c)
	if !ok {
		return
	}

	instance, err := service.GetHermesInstance(userID)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("failed to get hermes instance: %s", err.Error()))
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    toVO(instance),
	})
}

// CreateHermesInstance creates a new workspace (or returns the existing one).
func CreateHermesInstance(c *gin.Context) {
	userID, ok := getUserID(c)
	if !ok {
		return
	}

	instance, err := service.CreateHermesInstance(userID)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("failed to create hermes instance: %s", err.Error()))
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    toVO(instance),
	})
}

// startHermesReq is the camelCase (frontend convention) body for POST
// /instance/:instance_id/start. The service layer translates to the
// snake_case keys the manager expects.
type startHermesReq struct {
	ProviderAPIKey  string `json:"providerApiKey"`
	ProviderBaseURL string `json:"providerBaseUrl"`
	ProviderModel   string `json:"providerModel"`
}

func StartHermesInstance(c *gin.Context) {
	userID, ok := getUserID(c)
	if !ok {
		return
	}

	instanceID := c.Param("instance_id")
	if instanceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "instance_id is required",
		})
		return
	}

	var req startHermesReq
	// Empty body is allowed (wake without override). ShouldBindJSON leaves req
	// zero-valued on empty body; the manager 400s on first-start without a key,
	// which is the correct contract. We intentionally do not branch on the
	// bind error here — match repo style.
	_ = c.ShouldBindJSON(&req)

	if err := service.StartHermesInstance(userID, instanceID, req.ProviderAPIKey, req.ProviderBaseURL, req.ProviderModel); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("failed to start hermes instance: %s", err.Error()))
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

// RevokeHermesProviderKey clears the provider key snapshot at the manager.
func RevokeHermesProviderKey(c *gin.Context) {
	userID, ok := getUserID(c)
	if !ok {
		return
	}

	instanceID := c.Param("instance_id")
	if instanceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "instance_id is required",
		})
		return
	}

	if err := service.RevokeHermesProviderKey(userID, instanceID); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("failed to revoke hermes provider key: %s", err.Error()))
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

// GetHermesProviderState returns the provider source/model/key-set timestamp.
// The api_key itself is never returned (manager contract + struct shape).
func GetHermesProviderState(c *gin.Context) {
	userID, ok := getUserID(c)
	if !ok {
		return
	}

	instanceID := c.Param("instance_id")
	if instanceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "instance_id is required",
		})
		return
	}

	state, err := service.GetHermesProviderState(userID, instanceID)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("failed to get hermes provider state: %s", err.Error()))
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    state,
	})
}

func SleepHermesInstance(c *gin.Context) {
	userID, ok := getUserID(c)
	if !ok {
		return
	}

	instanceID := c.Param("instance_id")
	if instanceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "instance_id is required",
		})
		return
	}

	if err := service.SleepHermesInstance(userID, instanceID); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("failed to sleep hermes instance: %s", err.Error()))
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

// GetHermesAccessToken issues a short-lived workspace access token. The
// frontend uses it to open the workspace in a new tab via the manager-proxied
// workspace_url.
type hermesAccessTokenVO struct {
	Token        string `json:"token"`
	WorkspaceURL string `json:"workspaceUrl"`
	ExpiresAt    string `json:"expiresAt"`
}

func GetHermesAccessToken(c *gin.Context) {
	userID, ok := getUserID(c)
	if !ok {
		return
	}

	instanceID := c.Param("instance_id")
	if instanceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "instance_id is required",
		})
		return
	}

	token, err := service.GetHermesAccessToken(userID, instanceID)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("failed to get hermes access token: %s", err.Error()))
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": hermesAccessTokenVO{
			Token:        token.Token,
			WorkspaceURL: token.WorkspaceURL,
			ExpiresAt:    token.ExpiresAt,
		},
	})
}

// GetHermesManagerStatus reports manager reachability WITHOUT leaking the
// internal URL (it used to return data.url to the browser).
func GetHermesManagerStatus(c *gin.Context) {
	if !service.HealthCheckHermesManager() {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "hermes-manager is not available",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"status": "connected",
		},
	})
}

// EnsureHermesUser is an internal helper endpoint (auth-protected) that
// upserts the current user into manager. Can be called by the frontend on
// first Hermes page visit, or from the login flow.
func EnsureHermesUser(c *gin.Context) {
	userID, ok := getUserID(c)
	if !ok {
		return
	}

	if _, err := service.UpsertHermesUser(userID); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("failed to upsert hermes user: %s", err.Error()))
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

// StreamHermesMessage proxy handles streaming completions from the hermes-agent gateway FastAPI.
func StreamHermesMessage(c *gin.Context) {
	var req struct {
		Message   string `json:"message" binding:"required"`
		AgentType string `json:"agent_type" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}

	ctx := c.Request.Context()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Transfer-Encoding", "chunked")

	// Set up SSE channels
	streamChan := make(chan []byte)
	errChan := make(chan error)

	group := c.GetString("group")
	if group == "" {
		group = "default"
	}

	go func() {
		defer close(streamChan)
		defer close(errChan)
		service.CallHermesAgentStream(ctx, group, req.Message, req.AgentType, streamChan, errChan)
	}()

	c.Stream(func(w io.Writer) bool {
		select {
		case <-ctx.Done():
			return false
		case err, ok := <-errChan:
			if ok && err != nil {
				logger.LogError(ctx, fmt.Sprintf("hermes agent stream error: %s", err.Error()))
				return false
			}
		case data, ok := <-streamChan:
			if !ok {
				return false
			}
			msgStr := string(data)
			msgStr = strings.TrimSpace(msgStr)
			c.SSEvent("message", msgStr)
			return true
		}
		return false
	})
}
