---
description: Run piecemeal inside a WireGuard container's network namespace and forward its peer port through the VPN with NAT-PMP.
---

# VPN and port forwarding

piecemeal can run in a VPN container's network namespace, so all its traffic goes through the tunnel, and forward its peer port with NAT-PMP, as ProtonVPN and some other providers offer. Peers can then reach it from the internet without exposing your own address.

```yaml
services:
  wireguard:
    image: ghcr.io/linuxserver/wireguard
    cap_add: [NET_ADMIN]
    sysctls:
      - net.ipv4.conf.all.src_valid_mark=1
    ports:
      - 8080:8080
    volumes:
      - ./wg0.conf:/config/wg_confs/wg0.conf
    healthcheck:
      test: ["CMD-SHELL", "wg show | grep -q handshake"]
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

## Without a VPN

Without `PIECEMEAL_NATPMP_GATEWAY`, piecemeal asks your router to forward the peer port with UPnP or NAT-PMP. If the router doesn't support either, forward port 6881 (TCP and UDP) to the host yourself, or change `peerPort` in **Settings** and forward that.
