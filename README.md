# digger 🪣

A lightweight HTTP microservice that resolves DNS hostnames and returns plain-text lists of IPv4 addresses — one per line, sorted.

Built for network devices like MikroTik RouterOS that can run containers and make HTTP requests, but can't query DNS programmatically from scripts. Instead of hardcoding IP ranges that go stale, your scripts call digger and always get fresh addresses.

---

## Motivation 💡

Telegram, for example, continuously rotates its IPs across data centers. To route Telegram traffic through a VPN on a MikroTik router, you need an up-to-date list of IPs in an address-list. The router's scripting language can do HTTP, but not DNS. digger bridges that gap: it runs as a container on the router itself, accepts HTTP requests, resolves hostnames, caches results, and returns plain IP lists that scripts can parse line by line.

The same pattern applies to any device or automation that needs DNS results over HTTP.

---

## Quick Start 🚀

```bash
docker run -p 8080:8080 ghcr.io/strpc/digger:latest
```

```bash
curl "http://localhost:8080/resolve?host=t.me&host=telegram.org"
```

```
91.108.4.11
91.108.56.130
149.154.167.99
```

---

## API 📡

### `GET /resolve`

Resolves one or more hostnames and returns their IPv4 addresses, one per line, sorted.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `host` | yes | — | Hostname to resolve. Repeat for multiple hosts. |
| `cache` | no | `true` | Set to `false` to bypass cache read (result is still written to cache). |
| `ttl` | no | `300` | Per-request cache TTL in seconds. `0` always returns a fresh result. |

**Response:** `200 OK`, `Content-Type: text/plain`

**Response headers:**

- `X-Cache: HIT` — result served from cache
- `X-Cache: MISS` — result freshly resolved

**Errors:**

- `400` — no `host` parameter
- `404` — unknown path

**Examples:**

```bash
# Single host
curl "http://localhost:8080/resolve?host=t.me"

# Multiple hosts, custom TTL
curl "http://localhost:8080/resolve?host=t.me&host=telegram.org&ttl=600"

# Force fresh lookup, bypass cache
curl "http://localhost:8080/resolve?host=t.me&cache=false"
```

---

### `GET /health`

Returns version info and current cache state.

```
version=v1.0.0
commit=abc1234
cache_entries=3
```

---

### `GET /cache/clear`

Clears all cached entries.

```
cache cleared
```

---

## Configuration ⚙️

| Environment variable | Default | Description |
|----------------------|---------|-------------|
| `PORT` | `8080` | Port to listen on |
| `CACHE_TTL` | `300` | Default cache TTL in seconds |

---

## Docker Compose 🐳

```yaml
services:
  resolver:
    image: ghcr.io/strpc/digger:latest
    restart: unless-stopped
    ports:
      - "8080:8080"
    environment:
      CACHE_TTL: 300
      PORT: 8080
```

---

## Recipes 📖

- [**MikroTik + Telegram**](recipes/mikrotik-telegram.md) — run digger as a container directly on a MikroTik router and automatically route all Telegram traffic through a VPN tunnel.
- [**MikroTik — local image install**](recipes/mikrotik-local-image.md) — build the image for your router's architecture and install it manually via file transfer, no registry required.

---

## Development 🛠️

```bash
go run ./cmd/digger

# or build
go build -o digger ./cmd/digger
./digger
```

No external dependencies — stdlib only.

---

## Docker image 📦

Multi-platform image (`linux/amd64`, `linux/arm64`, `linux/arm/v7`) published to GitHub Container Registry on every version tag:

```bash
docker pull ghcr.io/strpc/digger:latest
docker pull ghcr.io/strpc/digger:v1.0.0
```

