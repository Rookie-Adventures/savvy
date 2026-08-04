# 2026-08-05 rebuild 丢 /opt/data：匿名卷未持久化导致 agent 会话/记忆消失

## 症状

用户打开工作区发现 agent 数据"丢了"——会话历史、记忆、配置全空。触发点是前一晚
scanner 自动 rebuild（`check_image_staleness` 标记 + `check_needs_rebuild` rm+create
换新镜像）之后。

## 根因

`create_container` 只给容器挂了 **`/workspace`（用户文件，命名卷）**，没挂 `/opt/data`。
`/opt/data` 是 hermes-agent 的 `HERMES_HOME`，存**会话 / 记忆 / config.yaml / cron / state.db**。

镜像 Dockerfile 把 `/opt/data` 声明成 `VOLUME` 但没指定名字 → Docker 每次 create 都给它
开一个**匿名卷**。匿名卷的生命周期绑定在容器上：

- `docker rm`（不带 `-v`）不删匿名卷 → 旧数据其实还躺在磁盘上；
- 但 `create_container` 重建容器时 Docker 又开一个**全新空匿名卷**挂到 `/opt/data`；
- 结果：`/workspace` 文件还在（命名卷），`/opt/data` 的会话/记忆凭空"消失"。

rebuild 路径（升级镜像、`needs_rebuild` 闭合）每次都走 rm+create，所以**每次自动换镜像
都会静默丢一次 agent 数据**。

## 抢救（数据找回）

旧匿名卷没被删，都在 `/var/lib/docker/volumes/`。按 config.yaml.bak 时间戳与容器创建时间
互证，匹配出 4 个用户重建前用的卷，拷入新建的命名卷：

| 用户 | 旧匿名卷（重建前 /opt/data） | 拷入命名卷 | 大小 |
|---|---|---|---|
| u1 | bf3d8511… | savvy-u1-opt | 7.2M |
| u2 | e376e756… | savvy-u2-opt | 8.0M |
| u14 | 7969422b… | savvy-u14-opt | 30M |
| u15 | 7d07e09a… | savvy-u15-opt | 7.5M |

拷贝用 `docker run --rm -v <旧>:/src -v <新>:/dst nginx:alpine sh -c "cp -a /src/. /dst/"`
（`cp -a` 保留属主，无需手动 chown）。

## 修复（根治）

`create_container` 的 volumes 增加 `/opt/data` 命名卷，按容器名派生 `{container_name}-opt`：

```python
volumes={
    volume_name: {"bind": "/workspace", "mode": "rw"},
    f"{container_name}-opt": {"bind": "/opt/data", "mode": "rw"},
}
```

命名卷在 `docker run` 不存在时自动创建，rebuild 重建容器时同名复用 → 数据不再丢。
新用户首启 Docker 会把镜像内 `/opt/data` 初始内容拷进空命名卷，行为不变。

## 改动清单

| 文件 | 改动 |
|---|---|
| `savvy-manager/app/docker_manager.py` | create_container 增挂 `{container_name}-opt -> /opt/data` |
| `savvy-manager/tests/test_docker_manager.py` | +test_create_container_mounts_opt_data_named_volume |
| 服务器数据 | 4 个用户旧匿名卷 → 命名卷（见上表） |

## 验证

- 单测：新挂载断言通过。
- 服务器：`create_container` 重建 savvy-u1-w1，`docker inspect` 确认
  `savvy-u1-w1-opt -> /opt/data`，容器内 `/opt/data` 见 SOUL.md / config.yaml / memories
  等找回数据。验证后容器已 stop（保持睡眠）。

## 已知限制 / 尾巴

- 旧的无名匿名卷（历史遗留，含更早的 agent 数据）仍留在磁盘，未清理。确认无需回溯后再
  `docker volume prune`。**prune 前务必确认没有容器还在引用、且数据已无回溯需求。**
- 若未来要迁移/备份用户 agent 数据，备份对象是 `savvy-uX-data` + `savvy-uX-opt` 两个命名卷。
