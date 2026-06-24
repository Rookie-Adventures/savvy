package controller

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

func GetHermesInstance(c *gin.Context) {
	userID := c.GetInt("id")
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "unauthorized",
		})
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
		"data":    instance,
	})
}

func StartHermesInstance(c *gin.Context) {
	userID := c.GetInt("id")
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "unauthorized",
		})
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

	err := service.StartHermesInstance(userID, instanceID)
	if err != nil {
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

func SleepHermesInstance(c *gin.Context) {
	userID := c.GetInt("id")
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "unauthorized",
		})
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

	err := service.SleepHermesInstance(userID, instanceID)
	if err != nil {
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

func GetHermesManagerStatus(c *gin.Context) {
	hermesManagerURL := os.Getenv("HERMES_MANAGER_URL")
	if hermesManagerURL == "" {
		hermesManagerURL = "http://localhost:8000"
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(hermesManagerURL + "/health")
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "hermes-manager is not available",
		})
		return
	}
	defer resp.Body.Close()

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"status": "connected",
			"url":    hermesManagerURL,
		},
	})
}
