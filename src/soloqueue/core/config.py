from typing import ClassVar

from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    """
    Core settings for SoloQueue.
    Loads from .env file and environment variables.
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

    model_config: ClassVar[SettingsConfigDict] = SettingsConfigDict(
        env_file=".env",
        env_file_encoding="utf-8",
        # Allow extra fields for future compatibility
        extra="ignore"
    )


# Global settings instance
settings = Settings()
