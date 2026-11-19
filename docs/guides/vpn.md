---
description: Run piecemeal inside a WireGuard container's network namespace and forward its peer port through the VPN with NAT-PMP.
---

# VPN and port forwarding

piecemeal can run in a VPN container's network namespace, so all its traffic goes through the tunnel, and forward its peer port with NAT-PMP, as ProtonVPN and some other providers offer. Peers can then reach it from the internet without exposing your own address.

When piecemeal can't run inside the VPN, send its traffic through the provider's [proxy](proxy.md) instead, with less coverage.

```yaml
services:
  wireguard:
    image: ghcr.io/linuxserver/wireguard
    cap_add: [NET_ADMIN]
    sysctls:
      - net.ipv4.conf.all.src_valid_mark=1
    environment:
      # The local networks the kill switch below lets through.
      KILLSWITCH_ALLOWED_NETWORKS: 192.168.1.0/24
    ports:
      - 8080:8080
    volumes:
      - ./wg0.conf:/config/wg_confs/wg0.conf
      - ./killswitch.sh:/custom-cont-init.d/killswitch.sh:ro
    healthcheck:
      test: ["CMD-SHELL", "wg show | grep -q handshake && nft list table inet killswitch > /dev/null"]
      interval: 30s

  piecemeal:
    image: ghcr.io/sebdanielsson/piecemeal:latest
    network_mode: service:wireguard
    depends_on:
      wireguard:
        condition: service_healthy
        restart: true
    environment:
      PIECEMEAL_NATPMP_GATEWAY: 10.2.0.1
      PIECEMEAL_BIND_INTERFACE: wg0
      PIECEMEAL_DISABLE_IPV6: "true"
    volumes:
      - piecemeal-data:/data
      - /srv/media/downloads:/downloads
```

- **`PIECEMEAL_NATPMP_GATEWAY`** is the VPN's NAT-PMP gateway: `10.2.0.1` for ProtonVPN. piecemeal asks it for a port, listens on whatever port it gets, renews the mapping before it expires, and moves when the VPN hands out a different port. Don't publish a peer port: it's on the VPN's side.
- **`PIECEMEAL_DISABLE_IPV6`** keeps peers to IPv4 when the tunnel only carries IPv4.
- **`/readyz`** answers 503 until the first port mapping exists, so an orchestrator can wait for it. The status bar in the web UI shows the port, and whether peers are connecting (connected, firewalled or disconnected).
- **The web UI and APIs** are published on the VPN container, since piecemeal shares its network. The \*arrs reach it through the VPN container's name or address.
- **`restart: true`** under `depends_on` restarts piecemeal when the VPN container is recreated, since the old network namespace goes away with it.

## Kill switch

Running in the VPN container's network keeps piecemeal's traffic in the tunnel while the tunnel exists. A tunnel that is up but dead is safe: traffic goes into it and is dropped. But when the WireGuard interface goes away, such as when `wg-quick down` runs or `wg-quick up` fails at start, its routing rules go with it, and the container's default route out of your own connection is left. Two layers keep piecemeal from falling back to your own address. Use both.

### In piecemeal: bind to the interface

`PIECEMEAL_BIND_INTERFACE=wg0` keeps every connection piecemeal makes for BitTorrent on that interface: peers over TCP and uTP, the DHT, trackers over HTTP, HTTPS and UDP, web seeds, their name lookups, and torrent files and RSS feeds from public addresses. Each socket is bound to the interface itself (`SO_BINDTODEVICE` on Linux, without extra privileges since Linux 5.7), so when the interface is down or gone, connections fail instead of taking another route.

- piecemeal won't start while the interface doesn't exist; a restart policy brings it back once it does.
- While it's down, the status bar says so, `/readyz` answers 503, and the qBittorrent API reports `disconnected`.
- When it comes back as a new interface, as WireGuard's does after `wg-quick up`, piecemeal restarts its torrent client on it. Torrents carry on.
- Addresses on `PIECEMEAL_FETCH_ALLOWED_NETWORKS`, such as Prowlarr's, are reached directly, and so is a name server on loopback, such as Docker's. NAT-PMP to the VPN's gateway and sign-in with SSO aren't bound either. With a [proxy](proxy.md), the connections to the proxy are bound.
- The WebRTC peers of WebTorrent and UPnP are off: they open sockets piecemeal can't bind.

### In the VPN container: a firewall

A firewall covers everything in the network namespace, whatever opens the socket. linuxserver's images run scripts in `/custom-cont-init.d` before the tunnel comes up; this one installs nftables rules that drop everything except loopback, the tunnel, WireGuard's own packets to the VPN server, and the local networks in `KILLSWITCH_ALLOWED_NETWORKS`. Nothing removes the rules, so they hold while the tunnel is down, missing or restarting. IPv6 leaves only through the tunnel: a tunnel that carries IPv6 keeps working, and one that doesn't can't leak it. Make the file executable.

```bash
#!/usr/bin/with-contenv bash
set -euo pipefail
# If the rules can't be installed, no internet at all rather than your own.
trap 'ip -4 route del default 2>/dev/null || true; ip -6 route del default 2>/dev/null || true' ERR
allowed4="" allowed6=""
for net in ${KILLSWITCH_ALLOWED_NETWORKS:-}; do
  case "${net}" in
    *:*) allowed6="${allowed6:+${allowed6}, }${net}" ;;
    *) allowed4="${allowed4:+${allowed4}, }${net}" ;;
  esac
done
{
  echo 'table inet killswitch'
  echo 'delete table inet killswitch'
  echo 'table inet killswitch {'
  echo '  chain output {'
  echo '    type filter hook output priority filter; policy drop;'
  echo '    oifname "lo" accept'
  echo '    oifname "wg*" accept'
  # WireGuard's packets to the VPN server carry wg-quick's fwmark.
  echo '    meta mark 0x0000ca6c accept'
  if [[ -n "${allowed4}" ]]; then echo "    ip daddr { ${allowed4} } accept"; fi
  if [[ -n "${allowed6}" ]]; then echo "    ip6 daddr { ${allowed6} } accept"; fi
  echo '    meta l4proto tcp reject with tcp reset'
  echo '    reject with icmpx admin-prohibited'
  echo '  }'
  echo '}'
} | nft -f -
```

`KILLSWITCH_ALLOWED_NETWORKS` must include the networks that reach piecemeal's web UI and APIs, or the replies are dropped: the Docker network the \*arrs and a reverse proxy come from, and your LAN. The healthcheck above requires the rules, so piecemeal doesn't start without them. To check it, run `wg-quick down wg0` in the VPN container: `curl https://example.com` there should fail at once, and work again after `wg-quick up wg0`.

## Without a VPN

Without `PIECEMEAL_NATPMP_GATEWAY`, piecemeal asks your router to forward the peer port with UPnP or NAT-PMP. If the router doesn't support either, forward port 6881 (TCP and UDP) to the host yourself, or change `peerPort` in **Settings** and forward that.
