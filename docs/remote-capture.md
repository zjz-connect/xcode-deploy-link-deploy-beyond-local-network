# Remote Capture

Document revision: 1.2.0-design.1
Status: isolated screenshot implementation; physical capture pending

## Scope

Add a screenshot operation to the existing daemon-owned Session using pinned
go-ios Instruments. The candidate CLI sends the new screenshot request over the
existing owner-only Unix control socket. The daemon returns original PNG bytes;
the client validates the PNG and saves a new owner-only file without cropping,
resizing or overwriting existing output. No WebDriverAgent is installed.

Screenshots use the existing verified device and userspace tunnel. They neither
open a new RemotePairing session nor read or modify the pairing record. Refuse
a concurrent install/uninstall instead of running capture later after a timeout.
Bound the request and close only the inner screenshot service on cancellation.
A screenshot failure must not reset the outer tunnel or installation status.

## Pairing and live-process boundary

The live daemon, LaunchAgent, profile and pairing record remain untouched during
this experiment. The branch is capture-existing-tunnel in a separate Git worktree.
Build/test directly with the cached patched dependency; do not run scripts/test.sh
because it invokes scripts/install.sh and changes the installed binary.

The existing installed daemon accepts only status, install, uninstall and stop.
It does not expose its negotiated remote IPv6/RSD endpoint and cannot hot-load
a screenshot handler. A new client can verify the old daemon rejects the command,
without restarting it. Do not extract process memory, scan ports, guess endpoints
or use stale logs to work around this explicit protocol boundary.

Updating the daemon is a separate activation step. It can use the same saved
RemotePairing identity without manual pairing. Its warm connection still closes
on process restart; reacquiring on 5G alone is not guaranteed, so activation must
happen when the iPhone exposes its listener on Wi-Fi.

## Validation

Verify PNG preservation, refusal to overwrite output, busy-session rejection and
that screenshot failure never closes the Session or changes install readiness.
Build the candidate into .build only, run the device-free suite and race checks,
then send one screenshot request to the old daemon to verify its capability
boundary and unchanged generation. Do not claim a physical screenshot from
a rejected request.

## Next capability

Remote XCTest can reuse testmanagerd and the signed Lyo Swift UI-test runner.
It first needs remote OS-version lookup instead of local usbmuxd, named PNG
attachment/results export, and a request lifecycle that owns the test session
without owning the tunnel. WebDriverAgent is optional for later interactive
Agent control and is not necessary for the existing deterministic capture tests.

## Candidate evidence — 2026-09-08

Candidate version: 1.2.0-capture-preview. The device-free suite, including a
Unix-socket screenshot round trip and byte-identical PNG output, passed with
`go test -race ./...`. `go vet ./...` and the candidate CLI build passed.
These tests use a fake device Session and are not physical capture acceptance.

One candidate screenshot request to the existing installed daemon returned
`control_request_invalid: unknown command` and created no output. Status before
and after remained active, generation 2, session_loss_count 1, install_count 40.
The daemon was not restarted and no pairing record or LaunchAgent was changed.
The raw bounded protocol evidence is local in .build/live-probe.json.

The existing dependency already contains go-ios screenshot support; this branch
adds the missing application command and Session integration. No dependency fork
or new pairing mechanism is introduced. Physical screenshot acceptance remains
pending activation of this candidate; dynamic XCTest acquisition is a later step.
