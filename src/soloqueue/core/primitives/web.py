import httpx
import html2text
from soloqueue.core.primitives.base import ToolResult, success, failure

def web_fetch(url: str, timeout: int = 30) -> ToolResult:
    """
    Fetch a URL and convert content to Markdown.
    """
    try:
        with httpx.Client(timeout=timeout, follow_redirects=True) as client:
            response = client.get(url)
            response.raise_for_status()
            
            # Simple content type check
            content_type = response.headers.get("content-type", "")
            
            if "text/html" in content_type:
                h = html2text.HTML2Text()
                h.ignore_links = False
                h.ignore_images = True
                markdown = h.handle(response.text)
                return success(markdown)
            else:
                # Return plain text for other types
                return success(response.text)
                
    except httpx.RequestError as e:
        return failure(f"Network error: {str(e)}")
    except httpx.HTTPStatusError as e:
        return failure(f"HTTP error {e.response.status_code}: {e.response.text}")
    except Exception as e:
        return failure(f"Fetch error: {str(e)}")
