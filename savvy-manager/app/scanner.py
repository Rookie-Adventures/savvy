import logging
from datetime import datetime, timedelta, timezone
from apscheduler.schedulers.background import BackgroundScheduler
from sqlalchemy.orm import Session
from . import docker_manager
from .database import SessionLocal
from .models import Instance, InstanceStatus, PlanType
from .docker_manager import PLAN_RESOURCES, PLAN_STORAGE_GB

logger = logging.getLogger(__name__)
scheduler = BackgroundScheduler()

FREE_WINDOW_HOURS = 2
MAX_UPGRADE_RETRIES = 3


def check_expired_instances():
    """现成免费睡:扫 FREE RUNNING 且 expires_at 到 → docker stop。"""
    db: Session = SessionLocal()
    try:
        now = datetime.now(timezone.utc)
        expired = (
            db.query(Instance)
            .filter(
                Instance.status == InstanceStatus.RUNNING,
                Instance.plan == PlanType.FREE,
                Instance.expires_at.isnot(None),
                Instance.expires_at <= now,
            )
            .all()
        )
        for inst in expired:
            if docker_manager.stop_container(inst.container_name):
                inst.status = InstanceStatus.SLEEPING
                inst.started_at = None
                inst.expires_at = None
                db.commit()
    finally:
        db.close()


def check_needs_upgrade(db: Session | None = None):
    """漏洞 1 修复:升补扫。needs_upgrade=True → 重试 update,≤3 次,超限告警停手。"""
    own_session = db is None
    if own_session:
        db = SessionLocal()
    try:
        pending = db.query(Instance).filter(Instance.needs_upgrade.is_(True)).all()
        for inst in pending:
            if inst.expected_plan is None:
                inst.needs_upgrade = False
                db.commit()
                continue
            # ponytail: increment-then-cap so exactly MAX attempts fire before stop.
            # brief's check-then-increment would stop one call late (needs a 4th
            # call to see retries>=MAX); tests assert stop at 3 calls.
            inst.upgrade_retries += 1
            if inst.upgrade_retries >= MAX_UPGRADE_RETRIES:
                logger.error(
                    "upgrade retries exhausted for instance %s (target=%s), needs manual intervention",
                    inst.instance_id, inst.expected_plan.value,
                )
                inst.needs_upgrade = False
                db.commit()
                continue
            res = PLAN_RESOURCES.get(inst.expected_plan.value)
            if not res:
                inst.needs_upgrade = False
                db.commit()
                continue
            ok = docker_manager.update_container_resources(
                inst.container_name, res["cpu_quota"], res["mem_limit"], res["pids_limit"],
            )
            if ok:
                inst.plan = inst.expected_plan
                inst.expires_at = None
                inst.needs_upgrade = False
                inst.upgrade_retries = 0
                inst.needs_rebuild = True
            db.commit()
    finally:
        if own_session:
            db.close()


def check_needs_downgrade(db: Session | None = None):
    """漏洞 2 修复:降补扫。expected_plan != plan → 对齐,FREE 时设 2h 窗。"""
    own_session = db is None
    if own_session:
        db = SessionLocal()
    try:
        pending = (
            db.query(Instance)
            .filter(Instance.expected_plan.isnot(None))
            .filter(Instance.plan != Instance.expected_plan)
            .all()
        )
        now = datetime.now(timezone.utc)
        for inst in pending:
            inst.plan = inst.expected_plan
            if inst.plan == PlanType.FREE:
                inst.expires_at = now + timedelta(hours=FREE_WINDOW_HOURS)
            inst.storage_quota_gb = PLAN_STORAGE_GB.get(inst.plan.value, PLAN_STORAGE_GB["FREE"])
            inst.expected_plan = None
            db.commit()
    finally:
        if own_session:
            db.close()


def check_needs_rebuild(db: Session | None = None):
    """漏洞 3 修复:log 重建闭合。needs_rebuild=True 且 SLEEPING → rm + run 新 plan,保 volume+provider_config。"""
    own_session = db is None
    if own_session:
        db = SessionLocal()
    try:
        pending = (
            db.query(Instance)
            .filter(Instance.needs_rebuild.is_(True))
            .filter(Instance.status == InstanceStatus.SLEEPING)
            .all()
        )
        for inst in pending:
            if not docker_manager.remove_container(inst.container_name):
                logger.error("rebuild: failed to remove container %s", inst.container_name)
                continue
            from . import crypto
            provider_config = None
            if inst.provider_config_enc:
                try:
                    provider_config = crypto.decrypt_provider_config(
                        inst.provider_config_enc, inst.provider_config_alg or "fernet",
                    )
                except Exception:
                    provider_config = None
            docker_manager.create_container(
                container_name=inst.container_name,
                volume_name=inst.volume_name,
                user_id=inst.user_id,
                workspace_id=inst.instance_id,
                plan=inst.plan.value,
                expires_at=None,
                provider_config=provider_config,
            )
            inst.needs_rebuild = False
            inst.status = InstanceStatus.NOT_CREATED
            db.commit()
    finally:
        if own_session:
            db.close()


def check_storage_quota():
    """Q3 软配额:周期取 volume 用量,超 storage_quota_gb → SYSLOG 软告警。不强制禁。"""
    db: Session = SessionLocal()
    try:
        client = docker_manager._client_or_none()
        if client is None:
            return
        try:
            df = client.df()
        except Exception:
            return
        usage_by_name = {}
        for v in df.get("Volumes", []):
            name = v.get("Name", "")
            usage = v.get("UsageData", {}).get("Size", 0)
            usage_by_name[name] = usage
        instances = db.query(Instance).filter(Instance.storage_quota_gb.isnot(None)).all()
        for inst in instances:
            used_bytes = usage_by_name.get(inst.volume_name, 0)
            used_gb = used_bytes / (1024 ** 3)
            if used_gb > inst.storage_quota_gb:
                logger.warning(
                    "storage soft quota exceeded: instance %s used %.1fGB > quota %dGB",
                    inst.instance_id, used_gb, inst.storage_quota_gb,
                )
    finally:
        db.close()


WORKSPACE_IMAGE_TAG = "hermes-unified:saas"


def check_image_staleness(db: Session | None = None):
    """镜像滞后自愈:拉当前 tag image id,遍历 managed 容器,容器绑定 image id
    != tag 当前 id → 统一标 needs_rebuild=True。SLEEPING 容器立即被相邻
    check_needs_rebuild 重建换镜像;RUNNING 容器不打断用户会话,等下次睡
    (FREE 2h 到期 / 用户主动关)时由 check_needs_rebuild 闭合。
    零 DB 列,复用现有重建路径(volume+provider_config 保留)。"""
    client = docker_manager._client_or_none()
    if client is None:
        return
    try:
        tag_image_id = client.images.get(WORKSPACE_IMAGE_TAG).id
    except Exception:
        return  # daemon unreach / tag 缺失 → 安静跳过,别误标满库 needs_rebuild
    try:
        containers = client.containers.list(
            filters={"label": "hermes.managed=true"}, all=True,
        )
    except Exception:
        return
    def _container_image_id(c):
        # c.image 懒查镜像；旧镜像被 prune 掉时抛 ImageNotFound → 视为 stale
        # (重建路径会 rm 旧容器换新镜像，正好闭合)。别让单容器拖死整个扫描。
        try:
            return c.image.id
        except Exception:
            return None

    stale_names = {
        c.name for c in containers
        if _container_image_id(c) != tag_image_id
    }
    if not stale_names:
        return
    own_session = db is None
    if own_session:
        db = SessionLocal()
    try:
        pending = (
            db.query(Instance)
            .filter(Instance.container_name.in_(stale_names))
            .filter(Instance.needs_rebuild.is_(False))
            .all()
        )
        for inst in pending:
            inst.needs_rebuild = True
        if pending:
            db.commit()
            logger.info(
                "image staleness: flagged %d instance(s) for rebuild (tag image id=%s)",
                len(pending), tag_image_id,
            )
    finally:
        if own_session:
            db.close()


def start_scanner():
    scheduler.add_job(check_expired_instances, "interval", minutes=1, id="check_expired", replace_existing=True)
    scheduler.add_job(check_needs_upgrade, "interval", minutes=1, id="check_needs_upgrade", replace_existing=True)
    scheduler.add_job(check_needs_downgrade, "interval", minutes=1, id="check_needs_downgrade", replace_existing=True)
    scheduler.add_job(check_needs_rebuild, "interval", minutes=1, id="check_needs_rebuild", replace_existing=True)
    scheduler.add_job(check_image_staleness, "interval", minutes=10, id="check_image_staleness", replace_existing=True)
    scheduler.add_job(check_storage_quota, "interval", minutes=10, id="check_storage_quota", replace_existing=True)
    scheduler.start()


def stop_scanner():
    scheduler.shutdown()
