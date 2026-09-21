package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

// SkillPayFulfill 付费履约（第⑨步）：用专用系统 token 走现有 /v1/chat/completions
// 转发链路完成一次 AI 问答，返回 content。失败不标记履约，Agent 重试时重新履约。
// 计费说明：每次履约消耗 SKILLPAY_RELAY_TOKEN 的额度（管理员为该 token 充值），
// 微信实收(0.1元/次)与额度消耗解耦，对账清晰。
func SkillPayFulfill(query string) (string, error) {
	if operation_setting.SkillPayRelayToken == "" || operation_setting.SkillPayRelayModel == "" {
		return "", fmt.Errorf("skillpay relay 未配置")
	}
	server := system_setting.ServerAddress
	if server == "" {
		return "", fmt.Errorf("ServerAddress 未配置")
	}

	reqBody, err := common.Marshal(map[string]interface{}{
		"model": operation_setting.SkillPayRelayModel,
		"messages": []map[string]string{
			{"role": "user", "content": query},
		},
		"stream": false,
	})
	if err != nil {
		return "", fmt.Errorf("marshal relay request: %w", err)
	}

	url := server + "/v1/chat/completions"
	client := &http.Client{Timeout: 120 * time.Second}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("new relay request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+operation_setting.SkillPayRelayToken)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("relay call: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		logger.LogError(context.Background(), fmt.Sprintf("skillpay fulfill relay failed: http=%d body=%s", resp.StatusCode, skillPayTruncate(string(raw), 300)))
		return "", fmt.Errorf("relay http %d", resp.StatusCode)
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := common.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("parse relay response: %w", err)
	}
	if len(out.Choices) == 0 || out.Choices[0].Message.Content == "" {
		return "", fmt.Errorf("relay response empty: %s", skillPayTruncate(string(raw), 300))
	}
	return out.Choices[0].Message.Content, nil
}
