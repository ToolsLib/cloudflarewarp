# Cloudflare client IP middleware for Traefik

This is the ToolsLib fork of BetterCorp’s archived Traefik plugin. [Upstream repository](https://github.com/BetterCorp/cloudflarewarp)

This middleware restores the visitor IP and scheme from Cloudflare headers supplied by a trusted proxy. Components that read `RemoteAddr` still see the immediate peer.

## Install from this checkout

For Traefik 3.8, mount or copy this repository to `plugins-local/src/github.com/BetterCorp/cloudflarewarp` relative to Traefik's working directory. In the official Traefik container, use `/plugins-local/src/github.com/BetterCorp/cloudflarewarp`. The directory follows the Go module name declared by this checkout. The catalog serves BetterCorp's upstream releases. [Traefik catalog entry](https://plugins.traefik.io/plugins/62e97498e2bf06d4675b9443/real-ip-from-cloudflare-proxy-tunnel)

Add the local plugin to Traefik's static configuration and restart Traefik. This example uses the file provider to load the middleware and router.

```yaml
entryPoints:
  web:
    address: ":80"

providers:
  file:
    filename: /etc/traefik/dynamic.yml

experimental:
  localPlugins:
    cloudflarewarp:
      moduleName: github.com/BetterCorp/cloudflarewarp
```

In `/etc/traefik/dynamic.yml`, attach the middleware to the router. Replace `http://whoami:80` with the URL of a backend reachable from Traefik.

```yaml
http:
  middlewares:
    cloudflarewarp:
      plugin:
        cloudflarewarp:
          disableDefault: false

  routers:
    whoami:
      rule: Path(`/whoami`)
      entryPoints: [web]
      middlewares: [cloudflarewarp]
      service: whoami

  services:
    whoami:
      loadBalancer:
        servers:
          - url: http://whoami:80
```

## Configure proxy trust

| Option | Default | Behavior |
| --- | --- | --- |
| `trustip` | Empty | Additional CIDR ranges for the proxy peers that connect directly to Traefik. Use `/32` for a single IPv4 address and `/128` for a single IPv6 address. |
| `disableDefault` | `false` | Set to `true` to disable the bundled Cloudflare edge ranges and use only `trustip`. |

For a Cloudflare-proxied public origin, keep `disableDefault: false` to include the bundled ranges. Restrict origin access to the intended Cloudflare traffic. [Bundled CIDRs](ips/ips.go) · [Cloudflare origin protection](https://developers.cloudflare.com/fundamentals/security/protect-your-origin-server/)

For Cloudflare Tunnel, configure the address Traefik actually sees for the `cloudflared` connector. For example, if the connector's fixed address is `172.20.0.10`, use the following middleware options and restrict the origin listener to the connector.

```yaml
disableDefault: true
trustip:
  - "172.20.0.10/32"
```

Keep Traefik's `forwardedHeaders.insecure` disabled. If the entry point already uses `forwardedHeaders.trustedIPs`, limit it to the immediate proxies you operate or trust. Treat `X-Is-Trusted` as diagnostic metadata and apply authentication separately. [Traefik entry point configuration](https://doc.traefik.io/traefik/reference/install-configuration/entrypoints/)

### Header validation and fallback

A trusted peer can supply one `CF-Connecting-IP` value containing a single IPv4 or IPv6 address. The middleware writes the canonical address to `X-Real-IP` and `X-Forwarded-For` and sets `X-Is-Trusted: yes`. With an untrusted peer or missing client IP, it sets `X-Is-Trusted: no`. An invalid or duplicate client IP from a trusted peer gets `X-Is-Trusted: danger`. In each fallback case, the request continues with the peer IP in `X-Real-IP`, the inherited `X-Forwarded-For` cleared, and any rejected `CF-Connecting-IP` removed. Traefik can append the peer to `X-Forwarded-For` when it proxies to the backend.

The scheme check runs independently. A single `CF-Visitor` header from a trusted peer can set `X-Forwarded-Proto` to `http` or `https`. Otherwise the middleware removes any rejected `CF-Visitor` and keeps the forwarding scheme established by the Traefik entry point.

## Update the bundled Cloudflare ranges

Run this from the repository root to compare the snapshot with Cloudflare's live list. A drift report lists added CIDRs with `+` and removed CIDRs with `-`. Drift and download or validation errors exit nonzero with distinct messages. [Cloudflare IP ranges API](https://developers.cloudflare.com/api/resources/ips/methods/list/)

```bash
go run ./internal/cmd/updatecfips
```

To apply a validated update, run `go run ./internal/cmd/updatecfips -write` and review the diff in `ips/ips.go` before committing it. GitHub Actions runs the read-only check weekly, and you can run **Cloudflare IP drift** manually before a release. The plugin uses the bundled snapshot at runtime. [Drift workflow](.github/workflows/cloudflare-ip-drift.yml)

## Development checks

CI uses Go 1.27.4 and golangci-lint 2.15.0. Run these checks from the repository root. The race detector requires a compatible C compiler.

```bash
go test -race ./...
golangci-lint run
go run golang.org/x/vuln/cmd/govulncheck@v1.10.0 ./...
bash test/test.sh
```

The integration test requires Docker with the Compose plugin. It starts pinned Traefik 3.8.0 and whoami 1.12.0 containers, loads the checkout as a local plugin, and checks IPv4 and IPv6 forwarding and fallback behavior with both YAML and TOML. It removes its containers and network on exit.

GitHub Actions runs the same suite for pull requests, branch pushes, and version tags. Coverage is available in the `go-coverage` workflow artifact. The tag run verifies the tagged checkout using the local plugin loader.
