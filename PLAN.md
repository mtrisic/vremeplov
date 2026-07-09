# Vremeplov — Plan

Read `SPEC.md` first (architecture + hardware reference) and `AGENTS.md`
(working rules + discrepancy log). This document records what has been built
and what optional work remains.

## Status: initial development complete

The original five-phase build (core machine with cycle-accurate video →
interactive TUI → program input paths → WASM frontend → docs/polish) is
**done and gated**: every phase ran against a mechanical gate (all module
tests in the devcontainer, `check-deps`, `check-wasm`, goldens) and was
reviewed before the next began. What exists today:

- `core/` — the machine: bus/memory map, per-T-state video pipeline,
  keyboard matrix + T-state-stamped event queue, tape deck with
  ROM-measured pulse timing plus a SAVE recorder (capture back to GTP)
  and a WAV codec (digitized-cassette input, audio export), TypeText,
  snapshots, determinism guarantees.
- `core/gtp/` — GTP cassette-image parser; `core/loader/` — the shared
  tape-image → running-program glue (wasm + desktop); `core/monitor` —
  the shared debugger engine every live frontend embeds.
- `cmd/headless` — the primary harness: frame/memory dumps, key scripts,
  tape/fast-load/typed program input, snapshots; golden-tested.
- `frontends/tui` — bubbletea frontend with adaptive renderers
  (half-block/quadrant/braille/text/scaled), sticky keys, Ctrl+X chrome,
  clipboard (bracketed paste in, OSC 52 screen-text copy out), and the
  monitor: a machine-language debugger panel (breakpoints,
  watchpoints, stepping, disassembly via gozilog `z80/disasm`, hex
  dump/poke) on top of core's debug API (`RunDebug`).
- `frontends/wasm` + `web/` — browser frontend (canvas, keyboard,
  `.gtp` picker, transport controls, paste-to-type, tape recording with
  download, and the same monitor/debugger panel via the shared
  `core/monitor` engine).
- `frontends/desktop` — native Ebiten GUI (window, sound by default,
  drag-and-drop tapes, system-clipboard copy-paste, footer chrome,
  docked monitor); the only cgo module, gated by
  `tools/check-desktop.sh` under xvfb.
- `examples/` — BASIC listings plus preserved `.gtp` games
  (credits/provenance in `examples/PROVENANCE.md`).
- CI: release binaries on `v*` tags (TUI cross-compiled; desktop built
  on ubuntu + macos runners), GitHub Pages (landing page +
  `/galaksija/` emulator) on pushes to `main`.

**Universal rules for future work** (unchanged): everything builds and tests
inside the devcontainer; work ends with a mechanical, verifiable gate and
stops for user review; small reviewable commits; never weaken tests; gozilog
gaps are proposed upstream, never worked around (AGENTS.md).

Container command forms (either is fine):

```sh
devcontainer up --workspace-folder .
devcontainer exec --workspace-folder . -- <cmd>
# or
docker build -f .devcontainer/Dockerfile -t vremeplov-dev .
docker run -d --name vremeplov-dev-ct \
  -v "$PWD":/workspaces/vremeplov -w /workspaces/vremeplov \
  vremeplov-dev sleep infinity
docker exec vremeplov-dev-ct <cmd>
```

## Optional future phases (planned, not scheduled)

Ordered roughly by recommended sequence; each would run as its own planned,
gated feature like the monitor/recorder/WAV work before it.

### 1. Usability batch (sound · copy-paste · warp · snapshots · screenshots)

Five small features sharing the frame-loop and chrome/controls plumbing:

- ~~Sound~~ — **done**: `core/audio.go` — `EnableAudio` +
  `RenderAudio(rate)` pull the tape-out DAC as a deterministic mono
  sample stream (the single mixing point a Galaksija Plus AY-3-8910
  would join); web Sound button streams it to Web Audio as scheduled
  buffers; the desktop frontend pulls the same API (sound on by
  default). TUI stays silent (terminals have no audio).
- ~~Copy-paste~~ — **done**, grown from the planned web paste-to-type
  into all three live frontends: web paste handler → `core.TypeText`
  (text fields keep native paste); TUI bracketed paste in + `^X y`
  OSC 52 screen-text copy out; desktop system clipboard both ways
  (`atotto/clipboard`, Cmd/Ctrl+V/C).
- **Warp speed**: run N emulated frames per real tick — a web button and
  a TUI chrome key; skip BASIC's slow moments. Pure frontend loop change.
- ~~Snapshot UI~~ — **done**: TUI `^X w`/`^X l` write/load
  `vremeplov-snap-N.gob` files; web Save/Load buttons download/restore
  the same gob format. (Alongside it the TUI chrome moved out of hiding:
  a clickable, wrapping footer button bar with the ^X keys on the
  labels.)
- ~~Screenshots~~ — **done**: the PNG writer moved into core
  (`FramePNG`, deterministic), TUI `^X c` writes `vremeplov-shot-N.png`
  (follows the full-frame toggle), web *Shot* downloads the active
  area — byte-identical to headless `--dump-frame --crop`.

### 2. ~~Rewind + reverse-step~~ — **done**

Shipped: `core/history.go` (snapshot ring + input journal + exact
journal-replayed `RewindTo`/`StepBack`), monitor `bs`/`rw`, TUI `^X b`,
web hold-to-rewind button. See SPEC §4.2.

### 3. Compatibility & hardware

- ~~Turbo GTP blocks (type 0x01)~~ — **done** (loaded as standard). The
  planned premise was wrong: MAME defines the type but *skips* it — no
  tool anywhere renders turbo pulses, and no stock ROM reads them
  (findings in SPEC §3.7 / AGENTS.md log 15). Turbo payloads share the
  standard block layout, so they now validate, fast-load, and play (at
  standard speed, through the ROM's own loader) everywhere.
- **Galaksija Plus**: the historical expansion — more RAM, hi-res video
  mode, AY-3-8910 PSG. A substantial project: second video path behind
  the same Frame API, PSG emulation (measured-first, like everything
  here), new machine config. MAME's `galaxyp` is the cross-reference.
- **Fast character-buffer video mode**: render 32×16 codes through chargen
  on demand (no per-T-state pipeline) as an opt-in speed mode; must not
  leak into core's architecture — a second renderer behind the same Frame
  API. (Only worth it if wasm performance ever pinches.)
- ~~Additional frontends: desktop GUI~~ — **done**: `frontends/desktop`,
  an Ebiten window (the repo's only cgo user) with pixel-perfect
  scaling, real key-down/up, sound on by default, drag-and-drop tape
  loading, the footer chrome (snapshots, screenshots, rewind, record,
  quit confirm), and the shared monitor docked beside the screen. The
  tape-picker glue moved to `core/loader` (shared with wasm). See
  SPEC §5.4; `tools/check-desktop.sh` is its gate.

### 4. Debugger polish

Conditional breakpoints (`b ADDR if REG=VAL`), ROM symbol names in the
disassembly window (sourced from Šolc's commented disassembly), a T-state
profiler/heatmap, and — once rewind lands — reverse-step in the monitor.
All contained in `core/debug.go` + `core/monitor`.

### 5. Exotic: microphone tape loading (web)

Web Audio microphone capture → the WAV decoder's pulse detector, live —
load a real 40-year-old cassette by playing it at the browser. The
decoder's tolerance bands were built for exactly this signal; needs a
streaming variant of `DecodeWAV` and a UI affordance. Pure demo value.

### 6. Distribution polish

- **TODO: sign + notarize the macOS release binaries** (and optionally
  sign the Windows ones). Today they ship unsigned, so Gatekeeper
  quarantines browser downloads and users must `xattr -d
  com.apple.quarantine` or click through Privacy & Security (documented
  in the README quickstart). Fixing it properly needs an Apple
  Developer Program membership, a Developer ID Application certificate,
  and `codesign` + `notarytool` steps (credentials as repo secrets) in
  the `desktop-macos` release job. A Homebrew cask/formula would also
  sidestep quarantine for `brew` users.

## Standing risks

- **INT pulse width unknown**: assumption documented in AGENTS.md log 1; only
  matters if software re-enables EI mid-pulse — revisit if a demo misbehaves.
- **tablix.org availability**: all facts already extracted into SPEC §3; use
  archive.org for anything further.
