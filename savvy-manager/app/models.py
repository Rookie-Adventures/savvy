from enum import Enum
from datetime import datetime, timezone
from sqlalchemy import Column, String, DateTime, Enum as SQLEnum, JSON, ForeignKey, Integer
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
    PAID_RESIDENT = "PAID_RESIDENT"


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
    started_at = Column(DateTime, nullable=True)
    expires_at = Column(DateTime, nullable=True)
    created_at = Column(DateTime, default=lambda: datetime.now(timezone.utc))
    updated_at = Column(DateTime, default=lambda: datetime.now(timezone.utc), onupdate=lambda: datetime.now(timezone.utc))

    workspace_state = relationship("WorkspaceState", backref="instance", uselist=False, cascade="all, delete-orphan")


class WorkspaceState(Base):
    __tablename__ = "workspace_states"

    instance_id = Column(String, ForeignKey("instances.instance_id", ondelete="CASCADE"), primary_key=True)
    state_data = Column(JSON, default=dict)
    last_synced_at = Column(DateTime, default=lambda: datetime.now(timezone.utc), onupdate=lambda: datetime.now(timezone.utc))
