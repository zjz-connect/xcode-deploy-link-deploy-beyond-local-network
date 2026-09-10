# Nodus Remote Deploy Architecture

Document revision: `1.2.0-design.3`

Revised: `2026-09-08`

## Decision

Nodus Remote Deploy is one unprivileged, launchd-managed Go daemon on macOS. It owns one
verified RemotePairing control connection, one TLS-PSK data connection, one
userspace RSD topology, and serialized app lifecycle requests for one
configured iPhone. Tailscale supplies stable unicast reachability but is not
pairing, session, installation, or removal authority.

The CLI is a short-lived client. It communicates only through a profile-scoped,
owner-only Unix socket. No control API is exposed over TCP.

The component name is also the runtime identity:

- executable and process: `nodus-remote-deploy`;
- per-user LaunchAgent: `com.zjz.nodus-remote-deploy`;
- runtime root: `~/Library/Application Support/Nodus Remote Deploy/`;
- runtime-root override: `NODUS_REMOTE_DEPLOY_RUNTIME_ROOT`.

The superseded executable, LaunchAgent, socket, runtime root and environment
key are removed at the v1.0.0 cutover. There is no command alias, parallel
daemon or compatibility path.

## Build and install ownership

| Stage | Authority |
| --- | --- |
| Source compilation | Xcode / `xcodebuild` |
| Development signing | Xcode and Apple's signing assets |
| Bridge reachability | Tailscale plus the configured iPhone Tailnet IP |
| Pair verification | The configured owner-only RemotePairing record |
| Warm session | The long-running Nodus Remote Deploy daemon |
| App transfer | Streaming zip conduit over the live RSD session |
| App removal | InstallationProxy for one explicit bundle identifier |
| Success readback | InstallationProxy bundle presence or absence enumeration |

Nodus Remote Deploy consumes an existing signed `.app`. It does not become an Xcode Run
Destination, select a signing team, modify a project, or weaken iOS signature
validation.

## Session lifecycle

```text
waiting_for_listener -> connecting -> active -> installing   -> active
                                      |    \-> uninstalling -> active
                              |       |            |
                              |       +------------+ service failure
                              |                    v
                              +--------------> recovering
                                                    |
                       later lifecycle operation ---+----> active

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
Git under `~/Library/Application Support/Nodus Remote Deploy/`.

## Security invariants

- Pairing records and profiles must be owner-only regular files.
- Pairing private key material is never copied into a profile, log, or response.
- The RSD-reported identifier must match the configured RemotePairing identifier.
- Install and uninstall requests share one serialization boundary. Install
  accepts only an existing code-signed `.app`; uninstall accepts only one exact
  syntactically valid bundle identifier and never a wildcard.
- Success requires post-install presence readback or post-uninstall absence
  readback.
- No command changes Tailscale ACLs, enables Developer Mode, edits Apple's USB
  pairing database, republishes Bonjour, or falls back to TestFlight.

## v1.1.0 cold-validation contract

The `uninstall --profile <profile> <bundle-id>` command removes one explicitly
named development app and its iOS data container through the daemon-owned warm
session. The short-lived CLI never opens a second RemotePairing tunnel. The
owner-only control request, InstallationProxy removal, and absence readback all
run under the same operation lock as installation. A service failure enters
`recovering` while retaining the outer tunnel; it is never reported as a
successful removal. The command owns no archive, backup, wildcard, bulk-delete,
or implicit current-app policy. Callers must treat successful removal as
irreversible local-data deletion and reinstall the desired signed app
explicitly.

Source commit `4aef0a7` implements this contract as Nodus Remote Deploy 1.1.0.
Repository unit tests, race tests, `go vet`, the pinned Link Core `ios`,
`tunnel`, `installationproxy`, and `zipconduit` tests, and an independent
versioned binary build passed. At that checkpoint the installed LaunchAgent ran the 1.1.0 binary.
Its first physical cold-removal attempt remains pending because iOS had not
reopened the configured RemotePairing listener after the preceding warm
generation was intentionally closed; no app or data container was removed in
that waiting state.

## v1.0.0 runtime acceptance

The canonical identity cutover passed on 2026-08-31 from source commit
`2c3a1ad72747c56aa8d8d54c44a2f2296da31cd1`:

- the pinned Link Core package tests, repository unit tests, race tests and
  `go vet` passed;
- the installed `nodus-remote-deploy` reported version `1.0.0`, the pinned Link
  Core commit and patch digest, and its binary SHA-256 was
  `21d34fd59e5cf8903c3017d67480e08a895b836ed8abc190c99fa5057834baa5`;
- the schema-4 profile was copied byte-for-byte with mode `0600`; `doctor`
  accepted build metadata, profile, pairing record, Tailnet peer and the live
  RemotePairing listener;
- `com.zjz.nodus-remote-deploy` acquired generation 1 and became `active` as
  the only loaded deployment job and process;
- the new daemon reinstalled the exact signed app already present on the phone,
  reached `InstallComplete` and `DataComplete`, performed its own bundle
  readback and advanced `install_count` to 1;
- CoreDevice independently read back the same product, bundle, version and
  build, then completed a terminate-existing foreground launch;
- only after that acceptance, the superseded LaunchAgent plist and runtime
  root were removed. The pairing record remained at its external authority.

## v1.2.0 capture candidate, design revision 6

Capture reuses the existing verified Session and returns raw PNG bytes over the
local control socket. The generation is recorded while the operation lock is
held. Build/test artifacts and the Go workspace belong to the current checkout;
installation and service activation are separate operations. The native XCTest
route replaces the earlier optional WebDriverAgent direction. On 2026-09-08,
user-authorized activation replaced the installed executable with capture preview
3 and restarted the same LaunchAgent without changing its profile or pairing.
The deployment session recovered; physical screenshot acquisition timed out.
See Remote Capture for the activation evidence and outstanding capture checks.

Preview 6 adds native testmanagerd execution to the same serialized session.
The authenticated RSD handshake supplies the device OS version. Selected tests
use the RSD testmanagerd, appservice and openstdio services, without local Xcode
discovery or usbmuxd. A private result directory holds structured completion
results and original PNG attachments. Client disconnect and timeout cancel only
the inner operation. Passing requires each selected method to finish exactly
once and every required PNG to exist and decode completely.

## Build metadata authority

Module version and pinned Link Core commit are Go constants consumed directly by
the CLI. The build script reads the same pinned commit for dependency checkout,
and injects only the actual downstream patch digest. The version command runs
the shared metadata validator before returning success, closing the previous
gap between a passing build/test and a doctor-rejected version.

Capture discovery revision 1.2.0-design.7: screenshot and XCTest operations obtain an identity-verified fresh RSD snapshot over their existing tunnel before using developer services. The acquisition map is installation bootstrap data, not permanent capture readiness. See remote-capture.md.
