import pytest
from fastapi.testclient import TestClient
from sqlalchemy import create_engine
from sqlalchemy.orm import sessionmaker
from app.main import app
from app.database import Base, get_db
from app.models import Instance, InstanceStatus, PlanType
from app.token import generate_access_token

# InMemory SQLite database for testing routers
SQLALCHEMY_DATABASE_URL = "sqlite:///./test_workspace.db"
engine = create_engine(SQLALCHEMY_DATABASE_URL, connect_args={"check_same_thread": False})
TestingSessionLocal = sessionmaker(autocommit=False, autoflush=False, bind=engine)


@pytest.fixture(name="db_session")
def fixture_db_session():
    Base.metadata.create_all(bind=engine)
    db = TestingSessionLocal()
    try:
        yield db
    finally:
        db.close()
        Base.metadata.drop_all(bind=engine)


@pytest.fixture(name="client")
def fixture_client(db_session):
    def override_get_db():
        try:
            yield db_session
        finally:
            pass

    app.dependency_overrides[get_db] = override_get_db
    yield TestClient(app)
    app.dependency_overrides.clear()


def test_validate_workspace_token_success(client, db_session):
    # 1. Create a dummy running instance
    instance = Instance(
        instance_id="inst-test-user",
        user_id="test-user",
        status=InstanceStatus.RUNNING,
        plan=PlanType.FREE,
        container_name="savvy-utest-user-w1",
        volume_name="savvy-utest-user-data"
    )
    db_session.add(instance)
    db_session.commit()

    # 2. Generate token
    token_data = generate_access_token("inst-test-user", "test-user")
    token = token_data["token"]

    # 3. Call validate endpoint
    response = client.get(
        "/internal/workspace/validate",
        headers={"X-Token": token}
    )

    assert response.status_code == 200
    assert response.headers.get("X-User-Id") == "test-user"
    assert response.headers.get("X-Instance-Id") == "inst-test-user"
    assert response.headers.get("X-Workspace-Upstream") == "http://savvy-utest-user-w1:3000"


def test_validate_workspace_token_missing_token(client):
    response = client.get("/internal/workspace/validate")
    assert response.status_code == 401
    assert response.json()["detail"] == "Missing token"


def test_validate_workspace_token_invalid_token(client):
    response = client.get("/internal/workspace/validate", headers={"X-Token": "invalid.token"})
    assert response.status_code == 401
    assert response.json()["detail"] == "Invalid or expired token"


def test_validate_workspace_token_instance_not_running(client, db_session):
    # 1. Create a dummy sleeping instance
    instance = Instance(
        instance_id="inst-test-user",
        user_id="test-user",
        status=InstanceStatus.SLEEPING,
        plan=PlanType.FREE,
        container_name="savvy-utest-user-w1",
        volume_name="savvy-utest-user-data"
    )
    db_session.add(instance)
    db_session.commit()

    # 2. Generate token
    token_data = generate_access_token("inst-test-user", "test-user")
    token = token_data["token"]

    # 3. Call validate endpoint
    response = client.get(
        "/internal/workspace/validate",
        headers={"X-Token": token}
    )

    assert response.status_code == 403
    assert response.json()["detail"] == "Instance is not running"
