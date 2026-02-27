from typing import ClassVar

from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    """
    Core settings for SoloQueue.
    Loads from .env file and environment variables.
    .env file values take precedence over system environment variables.
    """
    # System
    PROJECT_ROOT: str | None = None
    LOG_LEVEL: str = "INFO"
    REQUIRE_APPROVAL: bool = True
    
    # LLM
    OPENAI_API_KEY: str | None = None
    OPENAI_BASE_URL: str | None = None  # For DeepSeek/Local LLM
    DEFAULT_MODEL: str = "deepseek-reasoner"  # Default to DeepSeek R1
    
    # Feishu
    FEISHU_APP_ID: str | None = None
    FEISHU_APP_SECRET: str | None = None

    # Embedding
    SOLOQUEUE_EMBEDDING_ENABLED: bool = False
    SOLOQUEUE_EMBEDDING_PROVIDER: str | None = None
    SOLOQUEUE_EMBEDDING_MODEL: str | None = None
    SOLOQUEUE_EMBEDDING_API_BASE: str | None = None
    SOLOQUEUE_EMBEDDING_API_KEY: str | None = None
    SOLOQUEUE_EMBEDDING_DIMENSION: int | None = None

    model_config: ClassVar[SettingsConfigDict] = SettingsConfigDict(
        env_file=".env",
        env_file_encoding="utf-8",
        # Allow extra fields for future compatibility
        extra="ignore"
    )

    @classmethod
    def settings_customise_sources(cls, settings_cls, init_settings, env_settings, dotenv_settings, file_secret_settings):
        """Override source priority: .env file takes precedence over system environment variables."""
        return (init_settings, dotenv_settings, env_settings, file_secret_settings)


# Global settings instance
settings = Settings()
