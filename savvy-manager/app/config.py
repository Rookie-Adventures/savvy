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

    class Config:
        env_prefix = "SAVVY_"


settings = Settings()
