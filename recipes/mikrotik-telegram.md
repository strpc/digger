# Routing Telegram Traffic Through VPN on MikroTik

This guide walks through running the DNS resolver as a container directly on a MikroTik router and using it to route all Telegram traffic through a VPN tunnel.

---

## 1. Requirements

- **RouterOS** 7.4 or later (container support)
- **Architecture**: x86, arm64, or armv7 (CHR, RB5009, RB4011, hAP ax series, and others)
- **Prerequisites**:
  - Container mode enabled (requires a reboot after activation)
  - An existing VPN tunnel (WireGuard, OpenVPN, or any other — referred to as `wireguard1` in examples)
  - At least 64 MB free storage for the container image

---

## 2. Enable Container Mode

Container support is disabled by default and requires a device-mode change followed by a reboot.

```routeros
/system/device-mode/update container=yes
```

Physically press the reset button on the router when prompted (or power cycle it), then wait for it to boot.

Configure the container registry:

```routeros
/container/config
set registry-url=https://ghcr.io tmpdir=disk1/tmp
```

> Replace `disk1` with your actual storage name (`/file print` shows available paths).

---

## 3. Container Networking

Create a dedicated virtual Ethernet interface for the container:

```routeros
/interface/veth
add name=veth-resolver address=172.16.100.2/24 gateway=172.16.100.1
```

Create a bridge and attach the veth interface to it:

```routeros
/interface/bridge
add name=br-resolver

/interface/bridge/port
add bridge=br-resolver interface=veth-resolver
```

Assign an IP to the bridge — this becomes the gateway the container uses to reach the router's DNS:

```routeros
/ip/address
add address=172.16.100.1/24 interface=br-resolver
```

---

## 4. Run the Container

Pull and configure the container:

```routeros
/container
add remote-image=ghcr.io/strpc/digger:latest \
    interface=veth-resolver \
    dns=8.8.8.8 \
    start-on-boot=yes
```

**Optional:** override defaults (`PORT=8080`, `CACHE_TTL=300`) via environment variables:

```routeros
/container/envs
add name=resolver-env key=PORT value=9090
add name=resolver-env key=CACHE_TTL value=600

/container/set [find remote-image~"digger"] envlist=resolver-env
```

Start the container:

```routeros
/container/start [find remote-image~"digger"]
```

Verify it's running:

```routeros
/container/print
```

The container should be in `running` state. Test connectivity:

```routeros
/tool/fetch url="http://172.16.100.2:8080/health" output=user
```

Expected output: `cache_entries=0`

---

## 5. Static Telegram IP Subnets

Add these address-list entries once — they cover Telegram's DC servers and act as a fallback regardless of DNS.

> Source: https://github.com/blackmatrix7/ios_rule_script/blob/master/rule/Clash/Telegram/Telegram.yaml

```routeros
/ip/firewall/address-list
add list=list-telegram address=149.154.160.0/20  comment=static
add list=list-telegram address=91.108.0.0/16     comment=static
add list=list-telegram address=109.239.140.0/24  comment=static
add list=list-telegram address=5.28.192.0/18     comment=static
add list=list-telegram address=139.59.210.98/32  comment=static
add list=list-telegram address=196.55.216.167/32 comment=static
```

IPv6 (optional):

```routeros
/ipv6/firewall/address-list
add list=list-telegram address=2001:67c:4e8::/48   comment=static
add list=list-telegram address=2001:b28:f23c::/47  comment=static
add list=list-telegram address=2001:b28:f23f::/48  comment=static
add list=list-telegram address=2a0a:f280::/29      comment=static
```

---

## 6. DNS Resolver Script

Create a new script named `update-telegram-ips`:

```routeros
/system/script
add name=update-telegram-ips source={
    # --- config ---
    :local resolverUrl  "http://172.16.100.2:8080/resolve"
    :local tmpFile      "telegram-ips.txt"
    :local listName     "telegram"
    :local dnsComment   "dns-resolver"
    :local ipTtl        "00:30:00"

    # Source: https://github.com/blackmatrix7/ios_rule_script/blob/master/rule/Clash/Telegram/Telegram.yaml
    :local hosts {
        "t.me";"telegram.org";"telegram.me";"telegram.dog";"telegram.space";
        "cdn-telegram.org";"telegram-cdn.org";"telegra.ph";"legra.ph";"graph.org";
        "tx.me";"tg.dev";"stel.com";"tdesktop.com";"telega.one";"telesco.pe";
        "usercontent.dev";"comments.app";"mbrx.app";"contest.com";"quiz.directory";
        "telegramdownload.com";"api.imem.app";"api.swiftgram.app"
    }
    # --- end config ---

    # Build query string dynamically
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

> The only section you should need to edit is the `$hosts` array. All other parameters are in the config block at the top.

---

## 7. Scheduler

Run the script every 30 minutes, starting immediately on boot:

```routeros
/system/scheduler
add name=update-telegram-ips \
    on-event=update-telegram-ips \
    interval=30m \
    start-time=startup
```

To trigger it manually for the first time:

```routeros
/system/script/run update-telegram-ips
```

---

## 8. Route Telegram Traffic to VPN

Mark Telegram-destined packets in the mangle table:

```routeros
/ip/firewall/mangle
add chain=prerouting \
    dst-address-list=list-telegram \
    action=mark-routing \
    new-routing-mark=vpn-route \
    passthrough=no \
    comment=telegram-vpn
```

Add a routing rule that sends marked traffic through the VPN:

```routeros
/ip/route
add dst-address=0.0.0.0/0 \
    routing-table=vpn-route \
    gateway=wireguard1 \
    comment=telegram-vpn
```

> Replace `wireguard1` with your actual VPN interface name or peer IP address. For WireGuard the interface name works directly; for other VPN types you may need to specify the tunnel IP as the gateway.

---

## 9. Verify

**Check the address-list is populated:**

```routeros
/ip/firewall/address-list print where list=list-telegram
```

You should see both `static` entries (from step 5) and `dns-resolver` entries with a countdown timeout.

**Manually trigger the script:**

```routeros
/system/script/run update-telegram-ips
```

**Read the logs:**

```routeros
/log/print where topics~"info" message~"telegram-resolver"
```

Expected: `telegram-resolver: added 12 IPs` (count varies by DNS response)

**Test that a Telegram IP is routed through VPN:**

```routeros
/tool/traceroute 149.154.167.99 routing-table=vpn-route
```

---

## Notes

**Static subnets as fallback.** The `static` entries in the address-list ensure Telegram remains reachable even if the container is down or DNS fails. DNS entries auto-expire after 30 minutes via `timeout` and are refreshed by the scheduler.

**Forcing a fresh DNS lookup.** To bypass the cache in a single script run, append `&cache=false` to the URL:

```routeros
:local resolverUrl "http://172.16.100.2:8080/resolve?cache=false"
```

**TTL tuning.** The `timeout` on address-list entries (`ipTtl`) should be slightly longer than the scheduler interval so entries never expire between refreshes. The default of 30 minutes matches the 30-minute scheduler interval — increase `ipTtl` to `01:00:00` if you want a buffer.

**Container restarts.** With `start-on-boot=yes` the container comes up automatically after a router reboot. The scheduler's `start-time=startup` ensures the address-list is populated immediately without waiting for the first 30-minute tick.
