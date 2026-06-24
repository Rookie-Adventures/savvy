from enum import Enum
from datetime import datetime
from sqlalchemy import Column, String, DateTime, Enum as SQLEnum
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
    created_at = Column(DateTime, default=datetime.utcnow)
    updated_at = Column(DateTime, default=datetime.utcnow, onupdate=datetime.utcnow)


class Instance(Base):
    __tablename__ = "instances"

    instance_id = Column(String, primary_key=True)
    user_id = Column(String, index=True)
    status = Column(SQLEnum(InstanceStatus), default=InstanceStatus.NOT_CREATED)
    plan = Column(SQLEnum(PlanType), default=PlanType.FREE)
    container_name = Column(String)
    volume_name = Column(String)
    started_at = Column(DateTime, nullable=True)
    expires_at = Column(DateTime, nullable=True)
    created_at = Column(DateTime, default=datetime.utcnow)
    updated_at = Column(DateTime, default=datetime.utcnow, onupdate=datetime.utcnow)
