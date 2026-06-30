# syntax=docker/dockerfile:1

# --- build stage -------------------------------------------------------------
FROM golang:1.26-alpine AS build
WORKDIR /src

# cache delle dipendenze (layer separato): cambia solo se cambiano go.mod/go.sum
COPY go.mod go.sum ./
RUN go mod download

# sorgente + build statica (CGO off -> immagine finale senza libc)
# VERSION è iniettata nel binario via ldflags (default = linea pre-release corrente).
ARG VERSION=beta0.1
COPY . .
RUN CGO_ENABLED=0 GOFLAGS=-trimpath go build \
    -ldflags="-s -w -X xaltorka/version.Version=${VERSION}" -o /xaltorka .

# --- runtime stage (distroless, non-root) ------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot
ARG VERSION=beta0.1
LABEL org.opencontainers.image.title="xal-tor-ka" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.vendor="SFS.it di Zanutto Agostino"
COPY --from=build /xaltorka /usr/local/bin/xaltorka
EXPOSE 8080
# La directory di configurazione viene montata su /etc/xaltorka (vedi compose).
ENTRYPOINT ["/usr/local/bin/xaltorka"]
CMD ["-config", "/etc/xaltorka"]
