package router

import (
	"testing"

	"github.com/gin-gonic/gin"
)

// 路由重名在 gin 里是**运行时 panic**,编译期查不出来:
// 曾因 selfRoute 与 adminRoute 都挂在 /api/user 下注册了同路径,新构建一起容器就崩,
// 生产直接 crash-loop。这里在建包阶段就把注册跑一遍,重名立即失败。
func TestSetApiRouterHasNoDuplicateRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("SetApiRouter 注册路由时 panic(疑似路径重复): %v", r)
		}
	}()
	SetApiRouter(gin.New())
}
