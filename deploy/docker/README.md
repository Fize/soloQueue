# SoloQueue Docker Image

The image includes Go, Node.js/npm/pnpm, Python 3, and common command-line and build tools.

## Run the published image (recommended)

Use the published image; no source checkout or local build is required.
The `latest` tag follows stable releases. To pin a release, replace `latest`
with its Git tag, such as `vMAJOR.MINOR.PATCH`.

```bash
docker pull ghcr.io/fize/soloqueue:latest
docker volume create soloqueue-data
docker run -d --name soloqueue \
  -p 127.0.0.1:57689:57689 \
  -e DEEPSEEK_API_KEY="your-api-key" \
  -v soloqueue-data:/root/.soloqueue \
  ghcr.io/fize/soloqueue:latest
```

Open <http://127.0.0.1:57689>.

## Build from source (optional)

To run your own source changes, build a local image from the repository root:

```bash
docker build -f deploy/docker/Dockerfile -t soloqueue:local .
```

Then use the `docker volume create` and `docker run` commands above, replacing
`ghcr.io/fize/soloqueue:latest` with `soloqueue:local` in the run command.
