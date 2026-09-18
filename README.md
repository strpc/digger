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
| `subdomains` | no | `false` | Set to `true` to discover passive subdomains before resolving. Invalid boolean values return `400`. |

**Response:** `200 OK`, `Content-Type: text/plain`

**Response headers:**

- `X-Cache: HIT` — result served from cache
- `X-Cache: MISS` — result freshly resolved
- `X-Subdomains-Status: complete` — discovery completed, including when no names were found
- `X-Subdomains-Status: limited` — the global discovered-name limit was reached
- `X-Subdomains-Status: partial` — at least one provider failed or timed out, but already discovered names were retained
- `X-Subdomains-Status: degraded` — discovery failed or timed out before finding any names; only the original hosts were resolved
- `X-Subdomains-Source: direct` — at least one discovered name was retained
- `X-Subdomains-Source: none` — no discovered name was retained, or all inputs were IP literals

The `X-Subdomains-*` headers are only present when `subdomains=true`. Successful
`complete`, `partial`, and `limited` results are cached with their metadata;
degraded results are never cached. Cache entries for requests with and without
discovery are separate.

**Errors:**

- `400` — no `host` parameter
- `400` — invalid `subdomains` boolean
- `404` — unknown path

**Examples:**

```bash
# Single host
curl "http://localhost:8080/resolve?host=t.me"

# Multiple hosts, custom TTL
curl "http://localhost:8080/resolve?host=t.me&host=telegram.org&ttl=600"

# Force fresh lookup, bypass cache
curl "http://localhost:8080/resolve?host=t.me&cache=false"

# Discover passive subdomains, then resolve all unique IPv4 addresses
curl "http://localhost:8080/resolve?host=example.com&subdomains=true"
```

Subdomain discovery queries crt.sh, sub.md, and HackerTarget directly. sub.md and
HackerTarget work anonymously, or can use optional API keys from the environment;
crt.sh does not use a key. Each requested host is its own search boundary: no
public-suffix expansion is performed. Only ASCII DNS names (including punycode)
are accepted from discovery, and wildcard or out-of-bound names are discarded.

Providers run independently with an 8-second timeout, while the whole discovery
stage is bounded by `SUBDOMAIN_TIMEOUT`. Results from healthy providers survive a
timeout or failure elsewhere. Providers honor `Retry-After`; rate-limited
providers enter an in-memory cooldown (one hour when the server supplies no valid
delay). Anonymous upstream quotas still apply: sub.md documents 50 requests/day
and 1 request/second, while HackerTarget host search documents 20 requests/day
and at most 50 results/request. Provider failures and malformed responses are
written to the service log with the provider name; configured API keys are not
included in those messages.

Discovery keeps at most `SUBDOMAIN_LIMIT` new unique names across the request.
DNS lookups are limited to 32 concurrent operations process-wide and 3 seconds
each, including time waiting for a lookup slot.

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
| `SUBDOMAIN_LIMIT` | `500` | Positive global limit for newly discovered unique names per request; invalid values prevent startup |
| `SUBDOMAIN_TIMEOUT` | `20` | Positive overall subdomain-discovery timeout in seconds; invalid values prevent startup |
| `SUBMD_API_KEY` | — | Optional sub.md bearer token; anonymous access is used when unset |
| `HACKERTARGET_API_KEY` | — | Optional HackerTarget API key; anonymous access is used when unset |

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
      SUBDOMAIN_LIMIT: 500
      SUBDOMAIN_TIMEOUT: 20
      SUBMD_API_KEY: ${SUBMD_API_KEY:-}
      HACKERTARGET_API_KEY: ${HACKERTARGET_API_KEY:-}
      PORT: 8080
```

---

## Recipes 📖

- [**MikroTik + Telegram**](recipes/mikrotik-telegram.md) — run digger as a container directly on a MikroTik router and automatically route all Telegram traffic through a VPN tunnel.
- [**MikroTik — local image install**](recipes/mikrotik-local-image.md) — build the image for your router's architecture and install it manually via file transfer, no registry required.

---

## Development 🛠️

```bash
go version # Go 1.25 or newer
go run ./cmd/digger

# or build
go build -o digger ./cmd/digger
./digger
```

Subdomain discovery uses small internal HTTP adapters for crt.sh, sub.md, and
HackerTarget; provider details are not exposed to the HTTP or service layers.

---

## Docker image 📦

Multi-platform image (`linux/amd64`, `linux/arm64`, `linux/arm/v7`) published to GitHub Container Registry on every version tag:

```bash
docker pull ghcr.io/strpc/digger:latest
docker pull ghcr.io/strpc/digger:v1.0.0
```
