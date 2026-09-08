# Remote Capture

Document revision: 1.2.0-design.2
Candidate version: 1.2.0-capture-preview.2
Status: isolated implementation; activation and physical capture pending

## Direct acquisition route

Deploy Link already embeds pinned Link Core (go-ios). Use its Instruments
screenshot service on the daemon-owned, verified DeviceEntry and userspace tunnel.
Static capture returns the complete original PNG. Dynamic handbook acquisition
will use the signed Lyo Swift XCTest runner over testmanagerd, with named PNG
attachments and structured test results. WebDriverAgent is excluded from this
route; no WDA installation, server, HTTP bridge or general Agent control is planned.
Remote XCTest still needs an RSD OS-version path instead of local usbmuxd and
an attachment/result adapter before existing capture promotion can consume it.

## Screenshot protocol and ownership

The owner-only Unix socket carries one JSON final-result header terminated by
one newline, followed by exactly image_bytes raw PNG bytes on success. PNG is
not Base64-encoded or duplicated in JSON. The client uses the decoder's buffered
bytes and the remaining socket stream; rejects unexpected, oversized or truncated
bodies; validates the full PNG once; and creates one owner-only output without
overwriting existing files. Payloads are capped at 64 MiB and decoded images at
16 megapixels, with each dimension at most 16,384. No cropping or re-encoding.

The daemon takes the screenshot and captures its generation metadata under the
same operation lock. It refuses capture while another device operation owns the
session. A capture error neither closes nor reacquires the outer pairing tunnel,
nor changes installation readiness. Cancellation closes only the inner screenshot
service, once. A bounded socket write prevents a stalled client from keeping a
completed screenshot response open indefinitely. Service construction still uses
the pinned library's own bounded connection/channel operations; it is not a
context-aware constructor, and must be revalidated on the physical device.

## Build, test and activation

scripts/build.sh prepares the pinned dependency/cache and produces only the
worktree's .build/nodus-remote-deploy and .build/go.work. Its generated build
environment is also local to this worktree, so two checkouts cannot silently
reuse each other's Go workspace. It never replaces the installed executable.
scripts/test.sh invokes build and the relevant dependency/module/race/vet checks.
Only scripts/install.sh copies the candidate into the installed binary directory.
It never starts or restarts the LaunchAgent; activation remains a separate action.

The current user instruction requires an explicit later restart instruction after
app bug confirmation. Do not install or restart Deploy Link in this turn. Build
and test in capture-existing-tunnel. Preserve the live daemon, LaunchAgent, profile
and pairing record. The existing daemon has no screenshot handler and cannot
hot-load one. This protocol changes the unactivated candidate, without adding a
compatibility path to the old candidate format.

A future activation reuses the saved RemotePairing identity without manual
pairing. Restart still closes the warm connection; pure-5G reacquisition is not
guaranteed, so activate with the phone's RemotePairing listener available on Wi-Fi.

## Verification

Cover exact binary PNG round trips, truncated/oversized responses, image bounds,
refusal to overwrite files, busy-session refusal, and capture errors that leave
the existing session and generation intact. Validate build/test without any
installed executable replacement. These tests use fake sessions; physical
screenshot and dynamic capture acceptance remain pending service activation.

The previous candidate probe returned control_request_invalid on the installed
daemon and created no output. Its before/after status stayed active, generation 2,
session_loss_count 1. Do not repeat an unsupported screenshot probe for this revision.

## Candidate revision 2 evidence — 2026-09-08

scripts/test.sh completed successfully: candidate build/sign, relevant pinned
Link Core package tests, module tests, race checks and go vet all passed.
The binary round trip uses a PNG larger than the decoder read-ahead buffer;
the saved bytes match the source exactly. Frame and decoded-size rejection
checks passed. The local log is .build/capture-optimized-tests.log.

The installed binary fingerprint stayed unchanged through candidate build and
verification. The live service retained PID 957, generation 2, session_loss_count 1.
Lyo Swift iOS 0.14.2 was installed separately using that existing service, whose
final status was active, install_count 44. No Deploy Link installation or restart
was performed. Physical screenshot capture and dynamic XCTest remain unaccepted.
