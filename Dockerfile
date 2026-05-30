ARG VERSION=dev
ARG GIT_COMMIT=unknown

# Stage 1: Build frontend
FROM node:20-alpine AS frontend
WORKDIR /app
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Stage 2: Build backend
FROM golang:1.26-alpine AS backend
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# The embed directive (cmd/matrixctrl/assets.go) reads cmd/matrixctrl/dist —
# place the freshly built frontend there so the image never ships a stale UI.
COPY --from=frontend /app/dist ./cmd/matrixctrl/dist
ARG VERSION
ARG GIT_COMMIT
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s \
      -X github.com/bxnnyg/matrixctrl/internal/version.Version=${VERSION} \
      -X github.com/bxnnyg/matrixctrl/internal/version.Commit=${GIT_COMMIT}" \
    -o /matrixctrl ./cmd/matrixctrl

# Stage 3: Minimal runtime
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
COPY --from=backend /matrixctrl /usr/local/bin/matrixctrl
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/matrixctrl"]
