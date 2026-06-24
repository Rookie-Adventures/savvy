from datetime import datetime, timedelta, timezone
from apscheduler.schedulers.background import BackgroundScheduler
from sqlalchemy.orm import Session
from .database import SessionLocal
from .models import Instance, InstanceStatus, PlanType
from .docker_manager import stop_container

scheduler = BackgroundScheduler()


def check_expired_instances():
    db: Session = SessionLocal()
    try:
        now = datetime.now(timezone.utc)
        expired_instances = (
            db.query(Instance)
            .filter(
                Instance.status == InstanceStatus.RUNNING,
                Instance.plan == PlanType.FREE,
                Instance.expires_at.isnot(None),
                Instance.expires_at <= now,
            )
            .all()
        )

        for instance in expired_instances:
            if stop_container(instance.container_name):
                instance.status = InstanceStatus.SLEEPING
                instance.started_at = None
                instance.expires_at = None
                db.commit()
    finally:
        db.close()


def start_scanner():
    scheduler.add_job(check_expired_instances, "interval", minutes=1)
    scheduler.start()


def stop_scanner():
    scheduler.shutdown()
