# SoloQueue Docker Demo

This is a local demonstration stack, not a production deployment template.
It runs nginx and SoloQueue in one network namespace. SoloQueue listens on
`127.0.0.1:57647` inside that namespace, while nginx exposes the single demo
entry point on host `127.0.0.1:8080`.

Start it from the repository root:

```bash
docker compose -f deploy/docker-demo/compose.yaml up --build
```

Open <http://127.0.0.1:8080>. The nginx entry point proxies the Web Console,
REST API, WebSocket, Status UI, and health check. The demo does not configure
authentication, TLS, or external-domain CORS. Configure those policies in
your own ingress when deploying outside the local demo boundary.

Stop it with:

```bash
docker compose -f deploy/docker-demo/compose.yaml down
```
