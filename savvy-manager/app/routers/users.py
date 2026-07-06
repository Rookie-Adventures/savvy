from datetime import datetime, timedelta, timezone
from fastapi import APIRouter, Depends, HTTPException
from pydantic import BaseModel
from sqlalchemy.orm import Session
from ..auth import require_hmac
from ..config import settings
from ..database import get_db
from ..docker_manager import PLAN_STORAGE_GB
from ..models import Instance, InstanceStatus, PlanType, User

router = APIRouter(prefix="/internal/users", tags=["users"])


class UserUpsertResponse(BaseModel):
    user_id: str
    created: bool


class InstanceResponse(BaseModel):
    instance_id: str
    user_id: str
    status: InstanceStatus
    plan: PlanType
    container_name: str | None = None
    volume_name: str | None = None
    assigned_port: int | None = None
    started_at: str | None = None
    expires_at: str | None = None


@router.post("/upsert", response_model=UserUpsertResponse)
async def upsert_user(auth=Depends(require_hmac), db: Session = Depends(get_db)):
    user_id = auth["user_id"]
    existing_user = db.query(User).filter(User.user_id == user_id).first()
    created = existing_user is None

    if created:
        user = User(user_id=user_id, plan=PlanType.FREE)
        db.add(user)
        db.commit()
    else:
        existing_user.updated_at = datetime.now(timezone.utc)
        db.commit()

    return UserUpsertResponse(user_id=user_id, created=created)


@router.get("/{user_id}/instance", response_model=InstanceResponse)
async def get_instance(user_id: str, auth=Depends(require_hmac), db: Session = Depends(get_db)):
    if auth["user_id"] != user_id:
        raise HTTPException(status_code=403, detail="Not your instance")

    inst = db.query(Instance).filter(Instance.user_id == user_id).first()
    if not inst:
        return InstanceResponse(
            instance_id="",
            user_id=user_id,
            status=InstanceStatus.NOT_CREATED,
            plan=PlanType.FREE,
        )
    return InstanceResponse(
        instance_id=inst.instance_id,
        user_id=inst.user_id,
        status=inst.status,
        plan=inst.plan,
        container_name=inst.container_name,
        volume_name=inst.volume_name,
        assigned_port=inst.assigned_port,
        started_at=inst.started_at.isoformat() if inst.started_at else None,
        expires_at=inst.expires_at.isoformat() if inst.expires_at else None,
    )


@router.post("/{user_id}/instance", response_model=InstanceResponse)
async def create_instance(user_id: str, auth=Depends(require_hmac), db: Session = Depends(get_db)):
    if auth["user_id"] != user_id:
        raise HTTPException(status_code=403, detail="Not your user")

    existing = db.query(Instance).filter(Instance.user_id == user_id).first()
    if existing and existing.status not in (
        InstanceStatus.NOT_CREATED,
        InstanceStatus.DELETING,
        InstanceStatus.ERROR,
    ):
        return InstanceResponse(
            instance_id=existing.instance_id,
            user_id=existing.user_id,
            status=existing.status,
            plan=existing.plan,
            container_name=existing.container_name,
            volume_name=existing.volume_name,
            assigned_port=existing.assigned_port,
            started_at=existing.started_at.isoformat() if existing.started_at else None,
            expires_at=existing.expires_at.isoformat() if existing.expires_at else None,
        )

    instance_id = f"inst-{user_id}"
    container_name = f"savvy-u{user_id}-w1"
    volume_name = f"savvy-u{user_id}-data"

    user = db.query(User).filter(User.user_id == user_id).first()
    plan = user.plan if user else PlanType.FREE

    # 分配端口池中空闲端口
    used_ports = {
        p[0] for p in db.query(Instance.assigned_port)
        .filter(Instance.assigned_port.isnot(None)).all()
        if p[0]
    }
    assigned_port = next(
        (p for p in range(settings.workspace_port_start, settings.workspace_port_end + 1)
         if p not in used_ports),
        None,
    )
    if assigned_port is None:
        raise HTTPException(status_code=503, detail="No available workspace ports")

    inst = Instance(
        instance_id=instance_id,
        user_id=user_id,
        status=InstanceStatus.NOT_CREATED,
        plan=plan,
        container_name=container_name,
        volume_name=volume_name,
        assigned_port=assigned_port,
        storage_quota_gb=PLAN_STORAGE_GB.get(plan.value, PLAN_STORAGE_GB["FREE"]),
    )
    db.add(inst)
    db.commit()

    return InstanceResponse(
        instance_id=inst.instance_id,
        user_id=inst.user_id,
        status=inst.status,
        plan=inst.plan,
        container_name=inst.container_name,
        volume_name=inst.volume_name,
        assigned_port=inst.assigned_port,
    )
