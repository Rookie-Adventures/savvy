# 主站前端本地开发指南

> 留痕文档:改主站(`new-api/web/default`)页面/样式/文案的闭环流程。
> 一条链路:改源码 → rebuild new-api 镜像 → 刷新。不跑 dev server,不碰 compose。

## 1. 现状

### 1.1 服务拓扑(dev `docker-compose.yml`)

| 服务 | 容器端口 | 宿主映射 | 说明 |
|---|---:|---|---|
| `nginx` | 80 | `80:80`, `41000-41099:41000-41099` | 反代 + workspace 端口池 |
| `new-api` | 3000 | 未映射 | Go 主站,前端 `go:embed web/default/dist` 进二进制 |
| `savvy-manager` | 8000 | 未映射 | Python,workspace 容器生命周期 |
| `newapi-db` | 5432 | `5432:5432` | Postgres |
| `redis` | 6379 | `6379:6379` | new-api 共享状态 |
| `manager-db` | 5432 | `5433:5432` | Postgres |

容器内网 `savvy-net`。workspace 容器由 savvy-manager 通过 docker.sock 创建,加入 `savvy_savvy-net`。

### 1.2 前端构建链(prod 镜像内,这就是我们用的路径)

```
new-api/Dockerfile
  stage1 (oven/bun):  COPY web/default → bun install → bun run build → dist/
  stage2 (golang):    COPY dist → go build(new-api 二进制 //go:embed web/default/dist)
  stage3 (debian):    跑 new-api,embed 的 dist 经 :3000 对外
```

`main.go` 嵌入点:`//go:embed web/default/dist`(:38)、`//go:embed web/classic/dist`(:44)。

→ **改前端 = rebuild new-api 镜像**,前端在 stage1 重新 `bun run build`,Stage2 重新 embed。改后端 Go 同一次 rebuild 即可。

## 2. 闭环流程(改完就这一步)

```powershell
cd E:\savvy
docker compose up -d --build new-api
```

- `--build`:强制 rebuild new-api 镜像(前端 + Go 都重 build)
- `-d`:后台
- 只重建 new-api,其他服务(db/redis/manager/nginx)不动,数据卷保留
- 完成后浏览器访问 `http://localhost`(nginx:80),刷新看到新前端

刷新若仍是旧的:**Ctrl+F5 硬刷新**(浏览器缓存)。

## 3. 改动前先看一眼现状

```powershell
docker compose ps          # 确认 5 个服务 Up
docker compose logs -f new-api   # 看 new-api 是否健康
```

## 4. 如果 build 失败 / 起不来

```powershell
cd E:\savvy
docker compose logs new-api --tail 50      # 看报错
# 回滚到改动前:用 git 还原 src 改动,再 rebuild
git -C new-api/web/default stash            # 暂存当前改动
docker compose up -d --build new-api
# 确认无误后 git stash pop 还原改动继续试
```

## 5. 注意

- **prod 部署**走 `docker compose -f docker-compose.prod.yml up -d --build new-api`,同一条思路,只是另一个 compose 文件。
- **i18n 改动**:同 rebuild,在 stage1 build 时打包进 dist。
- **workspace 端口池 41000-41099**:nginx 直接暴露,不走 dev server。
- **不需要本机装 bun/node** —— 构建在容器内完成,宿主只要 docker。

## 6. 已经做的改动(本文档关联)

- 2026-07-05:补齐 `web/default/src/i18n/locales/{en,zh}.json` 中 Hermes 工作区 28 条文案,中文用户不再看到英文 fallback。
- 2026-07-05:将 `src/hooks/use-sidebar-data.ts` 里 Hermes Workspace 入口从 Personal 组移到 Chat 组(Playground → Chat → Hermes Workspace)。
- 未改 compose 任何文件;rebuild 后即可看到效果。
