# SoloQueue Docker Image

The image includes Go, Node.js/npm/pnpm, Python 3, and common command-line and build tools.

Run these commands from the repository root:

```bash
docker build -f deploy/docker/Dockerfile -t soloqueue:latest .
docker volume create soloqueue-data
docker run -d --name soloqueue \
  -p 127.0.0.1:57689:57689 \
  -e DEEPSEEK_API_KEY="your-api-key" \
  -v soloqueue-data:/root/.soloqueue \
  soloqueue:latest
```

Open <http://127.0.0.1:57689>.
