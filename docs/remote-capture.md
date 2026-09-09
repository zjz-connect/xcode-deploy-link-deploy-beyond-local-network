# Remote Capture

Document revision: 1.2.0-design.6
Candidate version: 1.2.0-capture-preview.6
Status: physical remote execution verification in progress

## Scope and transport

Deploy Link uses the pinned Link Core dependency on its existing authenticated
Tailnet userspace tunnel. Screenshot, testmanagerd, CoreDevice appservice and
openstdio receive the same verified DeviceEntry as installation. Device OS
version comes from the authenticated RSD handshake. Remote test execution does
not consult Xcode destinations or local usbmuxd. WebDriverAgent is not used.

Xcode builds and signs the app and its native XCTest runner. Deploy Link installs
both bundles, then executes explicitly selected UI test methods. Tap and swipe
are real XCTest events in that signed runner. This is a capture workflow, not a
persistent general-purpose phone control server.

## Screenshot

`screenshot --profile PROFILE --output NEW_FILE.png` returns the entire device
PNG without cropping, scaling or re-encoding. One newline-terminated JSON header
precedes exactly `image_bytes` binary bytes on the owner-only Unix socket. The
client preserves decoder read-ahead, checks payload size and PNG decoding, and
creates a new owner-only file. Payloads are capped at 64 MiB and 16 megapixels.

A screenshot failure closes only its inner service. It does not close or
reacquire the outer pairing session, or change installation readiness. A busy
device operation is rejected explicitly. The existing Instruments channel has
a five-second RPC deadline; an earlier timeout was not treated as proof that
increasing this deadline would fix device readiness.

## Native test command

```sh
nodus-remote-deploy run-tests \
  --profile /absolute/path/to/iphone.json \
  --app-id lyo.swift \
  --runner-id lyo.swift.uitests.xctrunner \
  --test-bundle LyoSwiftUITests.xctest \
  --test InteractionMotionCaptureTests/testCaptureTapPress \
  --test InteractionMotionCaptureTests/testCaptureSwipe \
  --require-attachment interaction-motion-interaction-tap-press-interaction-tap-press-activated \
  --require-attachment interaction-motion-interaction-swipe-interaction-swipe-revealed \
  --output /absolute/path/to/new-capture \
  --timeout 8m
```

Repeat `--test` for each selected `Class/testMethod`, and
`--require-attachment` for each required named PNG. The signed app and runner
must already be installed. The timeout is bounded to 30 seconds through 30
minutes. The daemon creates a new private directory containing `result.json`,
`runner.log` and an `attachments` directory. PNG bytes are preserved and receive
`.png` extensions; names, test identities and paths are retained in the report.

Success requires every selected method to finish exactly once with passed
status. Every required screenshot must exist and fully decode. A missing test,
failed method, cancelled run, collector error or missing required PNG fails the
command. Full error details remain in `result.json`; the CLI includes its path.
Launch success alone is never acceptance evidence.

## Ownership and completion

Device operations are serialized by the daemon. Client disconnect, timeout and
service shutdown cancel the test's inner sockets without altering pairing.
Startup waits observe cancellation, rejected runner authorization is an error,
and the openstdio UUID is read completely across fragmented network reads.
Runner startup failure closes stdio; run completion joins stdout and performs
bounded runner cleanup. Listener callbacks and logs are synchronized, and final
results are detached from later callbacks before serialization.

Active suite lifecycle lookup remains separate from completed-test attachment
lookup. This preserves late screenshots without treating an already finished
parent suite as active. A regression reproduces the actual parent/leaf finish
order that previously caused result collection to fail after passing tests.
Optional attachment metadata uses the NSKeyedArchiver null representation on
iOS 27 failure diagnostics. Decode absent/null `userInfo` as absent metadata;
retain strict parsing of actual dictionaries and required screenshot payloads.

## Build and activation

`scripts/build.sh` builds only the checkout's `.build/nodus-remote-deploy` and
its local Go workspace. `scripts/test.sh` checks the relevant pinned dependency
packages, module tests, race checks and vet; it never installs or restarts a
service. `scripts/install.sh` atomically replaces the installed executable.
Activation restarts the existing LaunchAgent separately, using the unchanged
profile and pairing record. Version and pinned commit have one Go authority;
the build injects the actual downstream patch digest.

The user authorized activation and physical network testing on 2026-09-09.
Verify that Wi-Fi exposes the RemotePairing listener before stopping the warm
service. Initial acquisition requires that listener; pure-cellular cold
acquisition remains unsupported. Neither a passing build nor local Xcode test
execution proves cross-network capture.

## Physical evidence

- Before modification, the live Deploy Link returned a full 1320 × 2868 PNG.
- Local Xcode baseline: tap and swipe passed, with five full-frame screenshots.
- Preview 4 remote execution: both test methods passed and five PNGs arrived,
  but the result correctly failed because nested suite completion triggered a
  collector error. Preview 5 removes that faulty lifecycle lookup.
- Preview 6 passed the build, affected dependency packages (including the
  attachment archive decoder), module/race checks and vet. It is installed and
  active using the same profile, pairing record and LaunchAgent.
- Lyo Swift iOS 0.16.1 (20) and its sealed native test runner were installed
  through Deploy Link. Tap and swipe passed with five original PNGs. After
  repairing the ScrollView identifier and waiting for animated jumps, the
  focused vertical scroll test passed with its original 1320 × 2868 PNG.
- A screenshot command issued during an active test returned `session_busy`
  without interrupting the test or replacing its output. Failed tests and
  capture requests left the deployment session active.
- After a period of inactivity, standalone Instruments screenshot again timed
  out after five seconds despite an established inner connection. Its cause
  remains under investigation. Native XCTest screenshot attachments use a
  separate developer service and are verified independently.
- The full three-method repeat passed at 19:26–19:29 UTC on 2026-09-09:
  three selected methods, zero failures and six complete 1320 × 2868 PNGs.
  Its report is `.build/remote-acceptance/cross-network/result.json`. The user
  confirmed the iPhone was on cellular. Tailnet ping independently confirmed a
  direct public route (332 ms), and the peer's public address differs from the
  Mac's public endpoints. The daemon remained active at generation 1 throughout.
  Sanitized network observations are in `network-observation.json` beside it.
- Standalone screenshot also timed out immediately after that successful run.
  A one-off inner-service probe will measure a complete response with an
  explicit bounded deadline, distinguishing delayed PNG delivery from no
  response. It must reuse the live forwarder and current RSD services, never
  open a second pairing session or change the production timeout speculatively.

Local evidence is under `.build/remote-acceptance`. Only a completed remote run,
its verified images and a distinct-network repeat fulfill acceptance.
