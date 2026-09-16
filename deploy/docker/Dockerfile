FROM node:22-alpine AS web-build

WORKDIR /src/web
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml ./
COPY web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY web ./
RUN pnpm build

FROM node:22-alpine AS status-build

WORKDIR /src/status-ui
RUN corepack enable
COPY status-ui/package.json status-ui/pnpm-lock.yaml ./
COPY status-ui/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY status-ui ./
RUN pnpm build

FROM golang:1.25-alpine AS go-build

WORKDIR /src
RUN apk add --no-cache ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
COPY --from=web-build /src/web/dist ./web/dist
COPY --from=status-build /src/status-ui/dist ./status-ui/dist
RUN mkdir -p internal/assets/dist \
    && cp -r web/dist internal/assets/dist/web \
    && cp -r status-ui/dist internal/assets/dist/status \
    && go build -ldflags="-s -w" -o /out/soloqueue ./cmd/soloqueue

FROM alpine:3.22

RUN apk add --no-cache ca-certificates
COPY --from=go-build /out/soloqueue /usr/local/bin/soloqueue
ENV HOME=/root
WORKDIR /root
EXPOSE 57689
ENTRYPOINT ["/usr/local/bin/soloqueue"]
CMD ["start", "--host", "0.0.0.0"]
