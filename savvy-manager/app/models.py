from enum import Enum
from datetime import datetime, timezone
from sqlalchemy import Column, String, DateTime, Enum as SQLEnum, JSON, ForeignKey, Integer, Text, Boolean
from sqlalchemy.orm import relationship
from .database import Base


class InstanceStatus(str, Enum):
    NOT_CREATED = "NOT_CREATED"
    CREATING = "CREATING"
    SLEEPING = "SLEEPING"
    STARTING = "STARTING"
    RUNNING = "RUNNING"
    STOPPING = "STOPPING"
    ERROR = "ERROR"
    DELETING = "DELETING"


class PlanType(str, Enum):
    FREE = "FREE"
    STARTER = "STARTER"
    PRO = "PRO"


class User(Base):
    __tablename__ = "users"

    user_id = Column(String, primary_key=True)
    plan = Column(SQLEnum(PlanType), default=PlanType.FREE)
    created_at = Column(DateTime, default=lambda: datetime.now(timezone.utc))
    updated_at = Column(DateTime, default=lambda: datetime.now(timezone.utc), onupdate=lambda: datetime.now(timezone.utc))


class Instance(Base):
    __tablename__ = "instances"

    instance_id = Column(String, primary_key=True)
    user_id = Column(String, index=True)
    status = Column(SQLEnum(InstanceStatus), default=InstanceStatus.NOT_CREATED)
    plan = Column(SQLEnum(PlanType), default=PlanType.FREE)
    container_name = Column(String)
    volume_name = Column(String)
    assigned_port = Column(Integer, nullable=True)
    provider_config_enc = Column(Text, nullable=True)
    provider_config_alg = Column(String(32), nullable=True)
    provider_key_set_at = Column(DateTime, nullable=True)
    needs_upgrade = Column(Boolean, default=False)
    needs_rebuild = Column(Boolean, default=False)
    expected_plan = Column(SQLEnum(PlanType), nullable=True)
    storage_quota_gb = Column(Integer, nullable=True)
    upgrade_retries = Column(Integer, default=0)
    started_at = Column(DateTime, nullable=True)
    expires_at = Column(DateTime, nullable=True)
    created_at = Column(DateTime, default=lambda: datetime.now(timezone.utc))
    updated_at = Column(DateTime, default=lambda: datetime.now(timezone.utc), onupdate=lambda: datetime.now(timezone.utc))

    workspace_state = relationship("WorkspaceState", backref="instance", uselist=False, cascade="all, delete-orphan")

    def __init__(self, **kwargs):
        # ponytail: SA default= fires at flush, not instantiation; set the
        # upgrade/rebuild defaults eagerly so newly constructed Instances have
        # sensible Python-level values before flush.
        kwargs.setdefault("needs_upgrade", False)
        kwargs.setdefault("needs_rebuild", False)
        kwargs.setdefault("upgrade_retries", 0)
        super().__init__(**kwargs)


class WorkspaceState(Base):
    __tablename__ = "workspace_states"

    instance_id = Column(String, ForeignKey("instances.instance_id", ondelete="CASCADE"), primary_key=True)
    state_data = Column(JSON, default=dict)
    last_synced_at = Column(DateTime, default=lambda: datetime.now(timezone.utc), onupdate=lambda: datetime.now(timezone.utc))
