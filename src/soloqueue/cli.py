"""SoloQueue CLI - Web Server Entry Point."""

import sys
import uvicorn
from soloqueue.web.config import web_config

def main():
    """Start the SoloQueue Web UI server."""
    # Simple direct startup. No argparse needed for now as we only have one function.
    # Future arguments can simply override web_config.
    
    print(f"🚀 Starting SoloQueue Web UI on http://{web_config.HOST}:{web_config.PORT}...")
    print(f"   (Press Ctrl+C to stop)")
    
    uvicorn.run(
        "soloqueue.web.app:app", 
        host=web_config.HOST, 
        port=web_config.PORT, 
        reload=web_config.DEBUG
    )

if __name__ == "__main__":
    main()
