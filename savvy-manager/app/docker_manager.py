import os

import docker
from docker.errors import NotFound, APIError, DockerException
from .config import settings

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
) -> dict:
    if settings.mock_mode:
        return {
            "id": f"mock-{container_name}",
            "name": container_name,
            "status": "created",
        }

    try:
        # Define resource limits and log rotation limits per plan
        # FREE: 0.5 CPU, 768m RAM, 128 pids, log 10m x 3
        # PAID_RESIDENT: 2.0 CPU, 2g RAM, 512 pids, log 20m x 5
        limits = {
            "FREE": {
                "mem_limit": "768m",
                "cpu_quota": 50000,
                "pids_limit": 128,
                "log_max_size": "10m",
                "log_max_file": "3"
            },
            "PAID_RESIDENT": {
                "mem_limit": "2g",
                "cpu_quota": 200000,
                "pids_limit": 512,
                "log_max_size": "20m",
                "log_max_file": "5"
            }
        }

        # Safe fallback to FREE if plan is invalid/unsupported
        limit_cfg = limits.get(plan, limits["FREE"])

        client = _client_or_none()
        if client is None:
            return {"error": "docker daemon unavailable (socket not mounted?)"}
        container = client.containers.run(
            "hermes-unified:saas",
            name=container_name,
            volumes={volume_name: {"bind": "/workspace", "mode": "rw"}},
            environment={"HERMES_ALLOW_INSECURE_REMOTE": "1"},
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
        return True
    except NotFound:
        return False
    except APIError:
        return False


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
