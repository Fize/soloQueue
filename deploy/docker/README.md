# SoloQueue Docker Image

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
