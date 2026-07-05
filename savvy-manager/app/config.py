from pydantic_settings import BaseSettings


class Settings(BaseSettings):
    app_name: str = "Savvy Manager"
    debug: bool = False

    # Database (PostgreSQL)
    database_url: str = "postgresql://savvy:savvy123@manager-db:5432/savvy_manager"

    # HMAC shared secret (must match new-api backend)
    hmac_secret: str = "dev-hmac-secret-change-me"

    # HMAC request max age in seconds
    hmac_max_age: int = 300

    # Mock mode: skip real Docker operations
    mock_mode: bool = True

    # Workspace 端口池（每实例分一个 nginx 监听端口，workspace 占根路径）
    workspace_port_start: int = 41000
    workspace_port_end: int = 41099
    # 返回给前端的公网 host（dev=localhost，prod=真实域名）
    public_host: str = "localhost"

    # Workspace 模型 provider 默认端点（首启注入 agent）。model 由 probe 从 new-api
    # /v1/models 拉取首项决定，不再有写死默认值（曾导致命中不存在的 channel→503）。
    openai_base_url: str = "http://new-api:3000/v1"
    # Fernet 加密用户 provider key 的密钥（32 字节 urlsafe base64）。缺失→fail-closed
    provider_enc_key: str = ""

    class Config:
        env_prefix = "SAVVY_"


settings = Settings()
