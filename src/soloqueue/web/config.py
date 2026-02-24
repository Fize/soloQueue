from pydantic_settings import BaseSettings

class WebConfig(BaseSettings):
    """Configuration for SoloQueue Web Server"""
    
    HOST: str = "0.0.0.0"
    PORT: int = 8000
    DEBUG: bool = True
    
    class Config:
        env_prefix = "SOLOQUEUE_WEB_"
        env_file = ".env"
        extra = "allow"

web_config = WebConfig()
