// Package antchain wraps the antchain restclient-go-sdk for order evidence
// submission. It is initialized once at startup via Init(); when ANTCHAIN_ENABLED
// is not "true", the package stays inert and Enabled remains false.
package antchain

import (
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/sirupsen/logrus"
	"gitlab.alipay-inc.com/antchain/restclient-go-sdk/client"
)

func init() {
	// SDK 的 init() 把 logrus 全局改成 JSON+stdout+InfoLevel, 每次合约调用都打
	// 一条 "request and resp" info 日志直灌 stdout, 绕过 new-api 自己的 logger。
	// 还原到静默: 丢弃输出 + 仅 Warn 以上, 别让 fire-and-forget 存证刷屏。
	// 段2a 调试时临时改成 stdout+Info 看回包, 已定位后还原。
	logrus.SetOutput(io.Discard)
	logrus.SetLevel(logrus.WarnLevel)
}

// Enabled reports whether the antchain client was successfully initialized.
var Enabled bool

var (
	restClient   *client.RestClient
	account      string
	tenantId     string
	bizId        string
	contractName string
	kmsId        string
	gas          int64
)

// Init reads ANTCHAIN_* environment variables, constructs the RestClient and
// performs the shake handshake. On any failure it logs via common.SysError and
// leaves Enabled=false so callers can nil-check the hook safely.
func Init() {
	if os.Getenv("ANTCHAIN_ENABLED") != "true" {
		return
	}

	accessId := os.Getenv("ANTCHAIN_ACCESS_ID")
	accessSecretFile := os.Getenv("ANTCHAIN_ACCESS_SECRET_FILE")
	restURL := os.Getenv("ANTCHAIN_REST_URL")
	kmsIdEnv := os.Getenv("ANTCHAIN_KMS_ID")
	accountEnv := os.Getenv("ANTCHAIN_ACCOUNT")
	contractEnv := os.Getenv("ANTCHAIN_CONTRACT_NAME")
	bizIdEnv := os.Getenv("ANTCHAIN_BIZ_ID")
	tenantIdEnv := os.Getenv("ANTCHAIN_TENANT_ID")
	gasEnv := os.Getenv("ANTCHAIN_GAS")

	if accessId == "" || accessSecretFile == "" || restURL == "" {
		common.SysError("antchain: missing required env (ACCESS_ID/ACCESS_SECRET_FILE/REST_URL)")
		return
	}

	// 私钥文件必须常驻: shake() 每次重试/202 重握手都会 ioutil.ReadFile 它。
	// 生产部署: 宿主机 /opt/savvy/secrets/antchain-access.key (chmod 600),
	// 容器只读挂载 -v ...:/secrets/antchain-access.key:ro, env 指向容器内路径。
	if _, err := os.Stat(accessSecretFile); err != nil {
		common.SysError("antchain: cannot stat ACCESS_SECRET_FILE=" + accessSecretFile + ": " + err.Error())
		return
	}

	// Defaults
	if accountEnv == "" {
		accountEnv = "savvy"
	}
	if contractEnv == "" {
		contractEnv = "savvy-solidity"
	}
	var gasVal int64
	if gasEnv != "" {
		if v, err := strconv.ParseInt(gasEnv, 10, 64); err == nil {
			gasVal = v
		}
	}

	// The SDK's NewRestClient reads a JSON config file. Build one from env vars.
	// AccessSecret is the file path — utils.Sign() calls ioutil.ReadFile internally.
	cfgJSON := fmt.Sprintf(`{
		"RestUrl":      %q,
		"AccessId":     %q,
		"AccessSecret": %q
	}`, restURL, accessId, accessSecretFile)

	tmpFile, err := os.CreateTemp("", "antchain-cfg-*.json")
	if err != nil {
		common.SysError("antchain: failed to create temp config: " + err.Error())
		return
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.WriteString(cfgJSON); err != nil {
		tmpFile.Close()
		common.SysError("antchain: failed to write temp config: " + err.Error())
		return
	}
	tmpFile.Close()

	rc, err := client.NewRestClient(tmpPath)
	if err != nil {
		common.SysError("antchain: shake failed: " + err.Error())
		return
	}

	restClient = rc
	account = accountEnv
	tenantId = tenantIdEnv
	bizId = bizIdEnv
	contractName = contractEnv
	kmsId = kmsIdEnv
	gas = gasVal
	Enabled = true
	common.SysLog("antchain: initialized successfully, contract=" + contractName)
}
