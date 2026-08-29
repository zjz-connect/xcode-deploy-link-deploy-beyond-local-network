# Deploy Link Architecture

Document revision: `0.7.0`

Revised: `2026-08-28`

## Decision

Deploy Link is one unprivileged, launchd-managed Go daemon on macOS. It owns one
verified RemotePairing control connection, one TLS-PSK data connection, one
userspace RSD topology, and serialized app-install requests for one configured
iPhone. Tailscale supplies stable unicast reachability but is not pairing,
session, or installation authority.

The CLI is a short-lived client. It communicates only through a profile-scoped,
owner-only Unix socket. No control API is exposed over TCP.

## Build and install ownership

| Stage | Authority |
| --- | --- |
| Source compilation | Xcode / `xcodebuild` |
| Development signing | Xcode and Apple's signing assets |
| Bridge reachability | Tailscale plus the configured iPhone Tailnet IP |
| Pair verification | The configured owner-only RemotePairing record |
| Warm session | The long-running Deploy Link daemon |
| App transfer | Streaming zip conduit over the live RSD session |
| Success readback | InstallationProxy bundle enumeration |

Deploy Link consumes an existing signed `.app`. It does not become an Xcode Run
Destination, select a signing team, modify a project, or weaken iOS signature
validation.

## Session lifecycle

```text
waiting_for_listener -> connecting -> active -> installing -> active
                              |          |            |
                              |          +------------+ service failure
                              |                       v
                              +-----------------> recovering
                                                       |
                                    later install -----+----> active

outer packet pump terminates -> session_lost -> waiting_for_listener
```

An InstallationProxy, ZipConduit, or RSD request failure does not by itself
destroy the outer warm tunnel. Only termination of the outer packet-dispatch
loop releases the session. This prevents a recoverable service error from being
misreported as successful connectivity while also avoiding self-inflicted cold
reacquisition.

## Network boundary

RemotePairing acquisition requires iOS to expose its listener. Any Wi-Fi
connection can activate it, and Tailscale lets the Mac reach the explicit
iPhone endpoint without Bonjour or a shared LAN. Once verified, both outer TCP
connections remain open while Tailscale changes underlay from Wi-Fi to cellular.

This is warm-session continuity, not pure-cellular discovery. If the outer
session terminates while the phone is cold on 5G, the daemon waits until Wi-Fi
causes iOS to expose RemotePairing again.

## Source and dependency boundary

- Public source mirror:
  `zjz-connect/xcode-deploy-link-deploy-beyond-local-network`;
- Git fetch authority: `/srv/contabo/git/deploy-link.git`;
- Go toolchain: `1.26.5` for macOS arm64;
- Link Core upstream: `danielpaulus/go-ios` commit
  `3ebc297691a9e364772aef027744ebc0c49421a5`;
- maintained downstream change: `patches/link-core-tailnet.patch`.

The installer verifies the Go archive checksum, verifies the exact upstream
commit, and fails if the patch no longer applies cleanly. Generated dependency
source, caches, profiles, pairing records, logs, and binaries remain outside
Git under `~/Library/Application Support/Deploy Link/`.

## Security invariants

- Pairing records and profiles must be owner-only regular files.
- Pairing private key material is never copied into a profile, log, or response.
- The RSD-reported identifier must match the configured RemotePairing identifier.
- Install requests are serialized and accept only an existing, code-signed
  `.app` directory.
- Success requires post-install bundle readback.
- No command changes Tailscale ACLs, enables Developer Mode, edits Apple's USB
  pairing database, republishes Bonjour, or falls back to TestFlight.
