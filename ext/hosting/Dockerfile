# syntax=docker/dockerfile:1
# Hosting extension UI — multi-stage, static Go binary on distroless:nonroot.
# Build context is the REPO ROOT (it compiles from the xaltorka module):
#   docker compose -f docker-compose.yml -f ext/hosting/docker-compose.yml build

FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# CGO off → fully static; -s -w strips symbols for a minimal image.
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /xtk-hosting-ui ./ext/hosting

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /xtk-hosting-ui /xtk-hosting-ui
EXPOSE 8090
# Runs as the image's non-root user (uid 65532). At runtime it is added to the
# xtk-agent group (compose group_add) so it can rw the 0660 agent socket.
ENTRYPOINT ["/xtk-hosting-ui"]
