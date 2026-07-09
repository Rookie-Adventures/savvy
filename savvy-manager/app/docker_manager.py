import os
import time


import docker
from docker.errors import NotFound, APIError, DockerException
from .config import settings

# Resource limits per plan (PRD §Plans And Resource Limits).
# cpu_quota is in microseconds per 100k period (50000 = 0.5 vCPU).
PLAN_RESOURCES = {
    "FREE":    {"cpu_quota": 50000,  "mem_limit": "768m", "pids_limit": 128},
    "STARTER": {"cpu_quota": 200000, "mem_limit": "2g",   "pids_limit": 512},
    "PRO":     {"cpu_quota": 400000, "mem_limit": "8g",   "pids_limit": 1024},
}
PLAN_LOG_CONFIG = {
    "FREE":    {"max-size": "10m", "max-file": "3"},
    "STARTER": {"max-size": "20m", "max-file": "5"},
    "PRO":     {"max-size": "50m", "max-file": "5"},
}
PLAN_STORAGE_GB = {"FREE": 5, "STARTER": 20, "PRO": 50}

# Lazily initialized so importing this module never touches the Docker daemon.
# (The container may run without a Docker socket mounted in mock mode.)
_client = None


def _get_client():
    """Return a Docker client, created on first use. None in mock mode."""
    global _client
    if settings.mock_mode:
        return None
    if _client is None:
        _client = docker.from_env()
    return _client


def _client_or_none():
    """Like _get_client() but returns None if the daemon is unreachable,
    so callers can degrade gracefully instead of raising."""
    try:
        return _get_client()
    except DockerException:
        return None


def _workspace_network() -> str | None:
    """Docker network that workspace containers must join so Nginx (running in
    the compose stack) can resolve them by container name. Configurable via env,
    defaults to the network that docker-compose creates for this project
    (<project>_<network-name> = savvy_savvy-net)."""
    return os.environ.get("SAVVY_WORKSPACE_NETWORK", "savvy_savvy-net")


def create_container(
    container_name: str,
    volume_name: str,
    user_id: str,
    workspace_id: str,
    plan: str,
    expires_at: str | None = None,
    provider_config: dict | None = None,
) -> dict:
    if settings.mock_mode:
        return {
            "id": f"mock-{container_name}",
            "name": container_name,
            "status": "created",
        }

    try:
        # Resource limits and log rotation per plan (see PLAN_RESOURCES / PLAN_LOG_CONFIG).
        # ponytail: PLAN_LOG_CONFIG uses docker-log-driver keys (max-size/max-file);
        # map to log_max_size/log_max_file so the run() call below stays unchanged.
        limit_cfg = {
            **PLAN_RESOURCES.get(plan, PLAN_RESOURCES["FREE"]),
            **{f"log_{k.replace('-', '_')}": v for k, v in PLAN_LOG_CONFIG.get(plan, PLAN_LOG_CONFIG["FREE"]).items()},
        }

        client = _client_or_none()
        if client is None:
            return {"error": "docker daemon unavailable (socket not mounted?)"}
        container = client.containers.run(
            "hermes-unified:saas",
            name=container_name,
            volumes={volume_name: {"bind": "/workspace", "mode": "rw"}},
            environment={
                "HERMES_ALLOW_INSECURE_REMOTE": "1",
                "HOST": "0.0.0.0",
                # 启用容器内 OpenAI-compat backend（:8642），workspace server 经
                # HERMES_API_URL=http://127.0.0.1:8642 调用。API_SERVER_KEY 是
                # api_server 鉴权密钥（≥16 字符），HERMES_API_TOKEN 是 workspace
                # 调用时携带的 bearer（两值必须一致）。回环内网，固定 secret 即可。
                "API_SERVER_ENABLED": "1",
                "API_SERVER_KEY": "saas-dev-api-server-secret-change-me-32byte",
                "HERMES_API_TOKEN": "saas-dev-api-server-secret-change-me-32byte",
                # dashboard backs the workspace's Sessions/Skills/Config APIs (:9119)
                "HERMES_DASHBOARD": "1",
                # dashboard bind 回环避开 auth gate（非回环需 auth provider）
                "HERMES_DASHBOARD_HOST": "127.0.0.1",
            },
            # CMD 保持 base 默认 TUI；tty=True 让 TUI 等待存续（仅占位保活）。
            # gateway + dashboard 由 s6 服务（Dockerfile.unified）supervise，独立于 CMD。
            tty=True,
            labels={
                "hermes.managed": "true",
                "user_id": user_id,
                "workspace_id": workspace_id,
                "plan": plan,
                "expires_at": expires_at or "",
            },
            detach=True,
            network=_workspace_network(),
            mem_limit=limit_cfg["mem_limit"],
            memswap_limit=limit_cfg["mem_limit"],  # memory swap = memory limit
            cpu_quota=limit_cfg["cpu_quota"],
            pids_limit=limit_cfg["pids_limit"],
            log_config={
                "type": "json-file",
                "config": {
                    "max-size": limit_cfg["log_max_size"],
                    "max-file": limit_cfg["log_max_file"]
                }
            },
            read_only=False,
            security_opt=["no-new-privileges:true"],
        )
        if provider_config is not None:
            from .provider_config import render_config_yaml
            _write_container_config_yaml(container, render_config_yaml(provider_config))
        return {"id": container.id, "name": container.name, "status": container.status}
    except APIError as e:
        return {"error": str(e)}
    except DockerException as e:
        # Daemon unreachable / socket not mounted: surface a clear error instead of 500.
        return {"error": f"docker daemon unavailable: {e}"}


def stop_container(container_name: str) -> bool:
    if settings.mock_mode:
        return True

    client = _client_or_none()
    if client is None:
        return False

    try:
        container = client.containers.get(container_name)
        container.stop()
        return True
    except NotFound:
        return False
    except APIError:
        return False


def start_container(container_name: str) -> bool:
    if settings.mock_mode:
        return True

    client = _client_or_none()
    if client is None:
        return False

    try:
        container = client.containers.get(container_name)
        container.start()
    except NotFound:
        return False
    except APIError:
        return False

    # ready: poll container.status until running (max 5 x 1s), then a fixed
    # 8s buffer for the workspace node server-entry to bind :3000. Timeout is
    # not fatal — the frontend shows "Starting…" until status flips to RUNNING,
    # so a slow start degrades to a wait, not a broken workspace shell.
    for _ in range(5):
        try:
            container.reload()
            if container.status == "running":
                break
        except (NotFound, APIError):
            break
        time.sleep(1)

    time.sleep(8)
    return True


def remove_container(container_name: str) -> bool:
    if settings.mock_mode:
        return True

    client = _client_or_none()
    if client is None:
        return False

    try:
        container = client.containers.get(container_name)
        container.remove(force=True)
        return True
    except NotFound:
        return False
    except APIError:
        return False


def list_managed_containers() -> list[dict]:
    if settings.mock_mode:
        return []

    client = _client_or_none()
    if client is None:
        return []

    try:
        containers = client.containers.list(
            filters={"label": "hermes.managed=true"},
            all=True,
        )
        return [
            {
                "id": c.id,
                "name": c.name,
                "status": c.status,
                "labels": c.labels,
            }
            for c in containers
        ]
    except APIError:
        return []


def _write_container_config_yaml(container, yaml_text: str) -> bool:
    """Write /opt/data/config.yaml inside the container via docker exec,
    using base64 to avoid shell-escape risks. Logs nothing that contains
    the api_key (yaml_text is never logged)."""
    import base64
    b64 = base64.b64encode(yaml_text.encode("utf-8")).decode("ascii")
    # Single exec: decode b64 to file. /opt/data is the hermes-agent HOME.
    cmd = ["sh", "-c", f"echo '{b64}' | base64 -d | cat > /opt/data/config.yaml"]
    try:
        result = container.exec_run(cmd)
        return getattr(result, "exit_code", 1) == 0
    except Exception:
        return False


def update_container_resources(
    container_name: str,
    cpu_quota: int,
    mem_limit: str,
    pids_limit: int,
) -> bool:
    """docker update 热改运行中容器资源。不重建,零中断。log_config 不改(留旧档,等 rebuild 闭合)。"""
    if settings.mock_mode:
        return True

    client = _client_or_none()
    if client is None:
        return False

    try:
        container = client.containers.get(container_name)
        # ponytail: pids_limit 不可热更 — docker SDK update_container() 不支持该 arg
        # (pids 限制仅在 create/HostConfig 时生效)。升级时只热改 cpu/mem,pids 差异
        # 由 needs_rebuild 路径(重建容器) 统一闭合。
        kwargs = dict(cpu_quota=cpu_quota, mem_limit=mem_limit,
                      memswap_limit=mem_limit)
        container.update(**kwargs)
        return True
    except NotFound:
        return False
    except APIError:
        return False
