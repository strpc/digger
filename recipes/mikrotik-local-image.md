# Building and Installing digger on MikroTik Without a Registry

Use this approach when the router has no internet access or you prefer not to pull from ghcr.io.

---

## 1. Determine Your Router's Architecture

| Device | Architecture |
|--------|-------------|
| RB4011, RB3011, hAP ac², RB941, RB951 | `linux/arm/v7` |
| RB5009, hAP ax², hAP ax³, Chateau ax | `linux/arm64` |
| CHR (Cloud Hosted Router) | `linux/amd64` |

Check in RouterOS if unsure:

```routeros
/system/resource/print
```

Look at the `architecture-name` field: `arm` → arm/v7, `arm64` → arm64, `x86_64` → amd64.

---

## 2. Build the Image

Install [Docker Desktop](https://www.docker.com/products/docker-desktop/) or the Docker CLI with Buildx support.

Clone the repository and build for your target platform:

```bash
# For arm/v7 (RB4011, RB3011, hAP ac², etc.)
docker buildx build \
  --platform linux/arm/v7 \
  --build-arg PROJECT_PATH=cmd/digger \
  --build-arg VERSION=$(git tag --points-at HEAD) \
  --build-arg COMMIT_HASH=$(git rev-parse --short HEAD) \
  --output type=docker,dest=digger-armv7.tar \
  .

# For arm64 (RB5009, hAP ax², hAP ax³, etc.)
docker buildx build \
  --platform linux/arm64 \
  --build-arg PROJECT_PATH=cmd/digger \
  --build-arg VERSION=$(git tag --points-at HEAD) \
  --build-arg COMMIT_HASH=$(git rev-parse --short HEAD) \
  --output type=docker,dest=digger-arm64.tar \
  .

# For amd64 (CHR)
docker buildx build \
  --platform linux/amd64 \
  --build-arg PROJECT_PATH=cmd/digger \
  --build-arg VERSION=$(git tag --points-at HEAD) \
  --build-arg COMMIT_HASH=$(git rev-parse --short HEAD) \
  --output type=docker,dest=digger-amd64.tar \
  .
```

This produces a `.tar` file — a self-contained Docker image archive ready to import.

---

## 3. Transfer the Image to the Router

Choose whichever method is most convenient.

### Option A — WinBox (drag and drop)

1. Open WinBox → **Files**
2. Drag the `.tar` file into the file list
3. Done — the file appears in `/` on the router

### Option B — FTP

MikroTik runs an FTP server by default on port 21.

```bash
ftp <router-ip>
# login with your RouterOS credentials
put digger-armv7.tar
bye
```

Or with `curl`:

```bash
curl -T digger-armv7.tar ftp://<router-ip>/ --user admin:<password>
```

### Option C — Fetch from a local HTTP server

Serve the file from your PC:

```bash
python3 -m http.server 8000
```

Then on the router:

```routeros
/tool/fetch url="http://<your-pc-ip>:8000/digger-armv7.tar" dst-path=digger.tar
```

---

## 4. Create and Start the Container

There is no separate import step — the `file=` parameter in `add` extracts the image and creates the container in one command.

Create the network (if not already done — see [mikrotik-telegram.md](mikrotik-telegram.md) steps 3–4):

```routeros
/interface/veth
add name=veth-resolver address=172.16.100.2/24 gateway=172.16.100.1

/interface/bridge
add name=br-resolver

/interface/bridge/port
add bridge=br-resolver interface=veth-resolver

/ip/address
add address=172.16.100.1/24 interface=br-resolver
```

Create the container from the uploaded tar file. Set `root-dir` to a path on your external storage:

```routeros
/container/add \
    file=digger.tar \
    interface=veth-resolver \
    root-dir=disk1/digger \
    dns=8.8.8.8 \
    start-on-boot=yes \
    name=digger
```

> Replace `disk1/digger` with the path on your external storage (e.g. `usb2-part1/docker/digger`). RouterOS will extract the image there — make sure the directory doesn't already exist.

**Optional:** override defaults (`PORT=8080`, `CACHE_TTL=300`) via environment variables:

```routeros
/container/envs
add name=resolver-env key=PORT value=9090
add name=resolver-env key=CACHE_TTL value=600

/container/set [find name=digger] envlist=resolver-env
```

The container will start extracting automatically. Check status:

```routeros
/container/print
```

Wait until status changes from `extracting` to `stopped`, then start it:

```routeros
/container/start [find name=digger]
```

Verify:

```routeros
/tool/fetch url="http://172.16.100.2:8080/health" output=user
```

Expected:

```
version=v1.0.0
commit=abc1234
cache_entries=0
```

---

## 6. Static Telegram IP Subnets

Add these once — they cover Telegram's DC servers and act as a fallback if DNS fails.

> Source: https://github.com/blackmatrix7/ios_rule_script/blob/master/rule/Clash/Telegram/Telegram.yaml

```routeros
/ip/firewall/address-list
add list=list-telegram address=149.154.160.0/20 comment=static
add list=list-telegram address=91.108.0.0/16    comment=static
add list=list-telegram address=109.239.140.0/24 comment=static
add list=list-telegram address=5.28.192.0/18    comment=static
add list=list-telegram address=139.59.210.98/32 comment=static
add list=list-telegram address=196.55.216.167/32 comment=static
```

---

## 7. DNS Resolver Script

Create a script named `update-telegram-ips`:

```routeros
/system/script
add name=update-telegram-ips source={
    # --- config ---
    :local resolverUrl  "http://172.16.100.2:8080/resolve"
    :local tmpFile      "telegram-ips.txt"
    :local listName     "telegram"
    :local dnsComment   "dns-resolver"
    :local ipTtl        "00:30:00"

    # Edit this list to add or remove hostnames
    # Source: https://github.com/blackmatrix7/ios_rule_script/blob/master/rule/Clash/Telegram/Telegram.yaml
    :local hosts {
        "t.me";"telegram.org";"telegram.me";"telegram.dog";"telegram.space";
        "cdn-telegram.org";"telegram-cdn.org";"telegra.ph";"legra.ph";"graph.org";
        "tx.me";"tg.dev";"stel.com";"tdesktop.com";"telega.one";"telesco.pe";
        "usercontent.dev";"comments.app";"mbrx.app";"contest.com";"quiz.directory";
        "telegramdownload.com";"api.imem.app";"api.swiftgram.app"
    }
    # --- end config ---

    :local query ""
    :foreach host in=$hosts do={
        :if ($query = "") do={
            :set query ("host=" . $host)
        } else={
            :set query ($query . "&host=" . $host)
        }
    }

    :local url ($resolverUrl . "?" . $query)
    /tool/fetch url=$url dst-path=$tmpFile
    /ip/firewall/address-list remove [find list=$listName comment=$dnsComment]
    :local content [/file get [find name=$tmpFile] contents]
    :local arr [:deserialize $content delimiter="\n" from=dsv options=dsv.plain]
    :local count 0
    :foreach row in=$arr do={
        :local line ($row->0)
        :if ([:len $line] > 0 && $line != "0.0.0.0") do={
            :do {
                /ip/firewall/address-list add list=$listName address=$line comment=$dnsComment timeout=$ipTtl
                :set count ($count + 1)
            } on-error={ :log warning ("telegram-resolver: skipped invalid: " . $line) }
        }
    }
    :log info ("telegram-resolver: added " . $count . " IPs")
    /file remove [find name=$tmpFile]
}
```

---

## 8. Scheduler

Run the script every 30 minutes, immediately on boot:

```routeros
/system/scheduler
add name=update-telegram-ips \
    on-event=update-telegram-ips \
    interval=30m \
    start-time=startup
```

Trigger it manually for the first time:

```routeros
/system/script/run update-telegram-ips
```

---

## 9. Route Telegram Traffic to VPN

```routeros
/ip/firewall/mangle
add chain=prerouting \
    dst-address-list=list-telegram \
    action=mark-routing \
    new-routing-mark=vpn-route \
    passthrough=no \
    comment=telegram-vpn

/ip/route
add dst-address=0.0.0.0/0 \
    routing-table=vpn-route \
    gateway=wireguard1 \
    comment=telegram-vpn
```

> Replace `wireguard1` with your VPN interface name or peer IP.

---

## 10. Verify

```routeros
# Address-list should contain both static and dns-resolver entries
/ip/firewall/address-list print where list=list-telegram

# Check logs
/log/print where topics~"info" message~"telegram-resolver"

# Test routing
/tool/traceroute 149.154.167.99 routing-table=vpn-route
```

---

## Updating

To update to a newer version:

1. Build a new tar and upload it via WinBox (steps 2–3).
2. Stop and remove the old container:

```routeros
/container/stop [find name=digger]
/container/remove [find name=digger]
```

3. Re-create from the new tar (RouterOS will overwrite the root-dir):

```routeros
/container/add \
    file=digger-new.tar \
    interface=veth-resolver \
    root-dir=usb2-part1/docker/digger \
    envlist=resolver-env \
    dns=8.8.8.8 \
    start-on-boot=yes \
    name=digger

/container/start [find name=digger]
```
