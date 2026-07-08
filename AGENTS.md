# AGENTS.md — working rules for Vremeplov

Vremeplov is a cycle-accurate Galaksija emulator in Go, built on
`github.com/mtrisic/gozilog`. Read `SPEC.md` (architecture + hardware
reference) and `PLAN.md` (completed build + optional future work) before doing
anything. These three documents are the complete project memory — assume no
chat history exists.

## Environment: devcontainer only

Every build, test, and result you report must be executed inside the
devcontainer (devcontainer CLI or `docker exec` — exact commands in `PLAN.md`
preamble). Never report a build/test result from a host toolchain. If it does
not work in the container, it is not done.

gozilog is consumed as a normal tagged module from GitHub
(`github.com/mtrisic/gozilog`); no local checkout is needed. WASM tests run
via `tools/check-wasm.sh` (Node + `wasm_exec.js`).

## Module purity

`core/` depends on the standard library and gozilog only — no `syscall/js`,
no OS-specifics, no third-party deps, no import of the `roms` module (core
takes `[]byte`; its tests read `roms/bin/` files via the shared testdata
helper). `tools/check-deps` enforces this and is part of every gate.

## gozilog co-development (optional)

Vremeplov depends on tagged gozilog releases. To co-develop against a local
gozilog checkout, add a local (uncommitted) workspace replace:
`go work edit -replace github.com/mtrisic/gozilog=/path/to/gozilog`
(in the devcontainer, bind-mount the checkout first). Rules when doing so:

- Integration gaps in gozilog are **never** worked around in Vremeplov and
  never vendor-patched. Propose a gozilog change instead.
- **Stop and ask the user for confirmation before any change to the gozilog
  working tree, and before any git commit to it.**
- Build/verify gozilog changes with gozilog's own devcontainer/Docker workflow
  (see gozilog's AGENTS.md).
- File every API gap, friction point, or bug as an issue on the gozilog
  repo — even ones you resolve locally.
- Never commit a `go.work`/`go.mod` that replaces gozilog with a local path.

## Testing invariants

- Never weaken, skip, or special-case a test to make it pass.
- Golden files (frames, memory dumps) are gate evidence; regenerate only via an
  explicit `-update` mechanism with the diff surfaced to the user.
- Determinism is a hard requirement: same ROMs + same timestamped inputs ⇒
  byte-identical results. Core has no wall clock, goroutines, or map-order
  dependence on any execution path.
- Present plans before large implementation steps; stop at every phase gate
  for user review. Small, reviewable commits per component.

## Hardware discrepancy & assumption log

When references disagree, prefer measured/documented sources (Šolc's thesis,
ROM disassembly, schematics) over anecdote, and record the discrepancy here.

| # | Topic | Status |
|---|---|---|
| 1 | **INT pulse width** is undocumented. Assumption: deassert on INT-ack, fallback deassert after 4 scanlines if never acknowledged (constant in core). Only observable if software runs with EI during the pulse tail. | Assumed; revisit if a demo misbehaves |
| 2 | **Tape comparator address**: thesis fig. 7 is ambiguous (comparator drawn in every row's first column); the ROM only ever reads `0x2000`. Emulator maps it at offset `0x00` only. | Assumed; cross-check schematic if tape bugs appear |
| 3 | **Latch reads** are undefined on hardware; emulator returns `0xFF`. | Chosen convention |
| 4 | **ROM B absent**: socket reads modeled as `0xFF` (floating bus). | Chosen convention |
| 5 | **A7 clamp scope**: modeled globally on the RAM address decode (all CPU + refresh accesses), per the disassembly header ("forces RAM address line A7 to one"). Stock ROM only enables it while executing from ROM. | Modeled per docs; schematic cross-check pending |
| 6 | **Pre-increment R on the refresh bus**: gozilog follows SingleStepTests (real-hardware measurements) over the classic datasheet; this is what the Galaksija circuit physically latches. | Resolved — matches hardware |
| 7 | **Šolc's ROM listing timing typos**: `ld (hl),a` annotated 10 T and `jp nz` 12 T in the scanline loop; datasheet values 7 and 10 are correct and make the loop exactly 192 T. Don't "fix" the emulator to match the annotations. | Resolved |
| 8 | **`SetINT` called from inside `Tick`** (reentrancy) — originally not a documented gozilog guarantee. | Resolved — documented and test-pinned upstream in gozilog v1.1.1 |
| 9 | **Chargen data-line wiring**: the thesis sentence about which data line is unconnected is garbled. Inspection of the actual ROM image (letters at glyph 0x01–0x1A, graphics in the upper half) shows **D6 is unconnected**: glyph = `(code&0x3F) \| ((code&0x80)>>1)`. | Resolved empirically from the ROM binary |
| 10 | **Shift-register bit order**: Šolc says "highest bit first", but the ROM file's glyphs mirror under that reading. Emulator shifts **LSB first** (bit 0 = leftmost pixel), which renders the character set correctly. | Resolved empirically; revisit only if a demo relies on the opposite order |
| 11 | **ROM A v29 requires ROM B**: the belief that v29 "auto-initializes ROM B if present" is wrong. Binary diff v28→v29 shows exactly one code change: the boot-time screen clear (`ld a,0x0C / rst 20h` at 0x03F9) is replaced by an **unconditional `CALL 0x1000`**. With an empty socket (0xFF bus) the CPU executes RST 38h forever and never reaches READY. Default machine therefore uses **v28**; v29 is only valid paired with ROM B. | Resolved by binary diff + boot test |
| 12 | **Tape timing**: documentation says "≈650 T half-pulses, ~330 bit/s". Measured T-state-exactly from ROM A v28's own SAVE (`core/tape_test.go`): pulse = **662 T high + 662 T low + mid rest**; "0" cell **9377 T**, "1" cell **9423 T** (second pulse at +4705 T), **+13421 T** interbyte pause (leader included, so effective rate ≈327 bit/s), 96-byte zero leader, LSB first. Interbyte gaps jitter ±10 T by ROM code path. The deck compiles schedules from these constants and the ROM's OLD reads them back (roundtrip test). | Resolved by measurement |
| 13 | **GTP trailing garbage**: a standard block's payload may carry extra byte(s) after the checksum (win11check.gtp has one; SAVE writes a garbage byte after the checksum on real tape). Parser validates through the checksum and ignores the tail; faithful playback plays the tail bytes as stored. | Resolved from samples |
| 14 | **`DumpMemory` is the CPU-eye view**: with latch b7=0 (e.g. machine stopped mid-ISR) the A7 clamp aliases 0x2800–0x3FFF reads with A7 clear onto A7 set. Program-memory equality checks in tests read raw RAM instead. | Chosen semantics; documented on the method |
| 15 | **GTP turbo blocks (0x01) have no waveform definition anywhere**: MAME (the presumed reference), GALe, and z88dk all *skip* them; the one consumer that reads them (pbakota/galaksijaemu, descended from Jevremović's emulator) fast-loads the payload with the **identical layout to a standard block**. The only pulse-level "turbo" in existence is ROM C's QSAVE/QLOAD (third ROM at 0xE000; PWM cells ≈411 T halves for "1", ≈1323 T for "0", read against a ≈784 T threshold), whose wire format doesn't match the GTP payload and which no stock ROM reads. Emulator loads turbo blocks exactly like standard ones (fast-load + standard-speed playback). | Resolved from source survey; SPEC §3.7 |
| 16 | **Wild WAV pulse widths ≫ the ROM's own**: circulating "digitized cassette" WAVs are mostly *synthesized* by GTP→WAV tools (perfectly uniform pulse widths give them away) with halves of 26 samples @ 44.1 kHz ≈ **1811 T** (archive.org `galaksija-wav` collection, e.g. Snake 2) or 30 samples ≈ **2090 T** (MAME `gtp_cas.cpp`), vs. SAVE's measured 662 T. They load on real hardware because the ROM's bit reader is **edge-triggered** and samples ~4360 T into the cell — pulse width is irrelevant to it. `wavMaxPhaseT` therefore spans 2200 T (below the 4705/2 ≈ 2352 T overlap ceiling for standard cell timing); cell timing in those files (splits ≈4500 T, zero cells ≈9000 T, interbyte ≈18–23k T) sits within the decoder's existing gap thresholds. | Resolved by measurement (Snake 2.wav decodes checksum-OK, loads and runs) |

## Pointers

- `SPEC.md` §9 — all source URLs (tablix.org is intermittently down; every page
  is on web.archive.org).
- `roms/PROVENANCE.md` — ROM origins, mejs commit SHA, checksums, licensing note.
- Separate project `galaksija-local-dev` (host path outside this repo) is
  **out of scope**: do not link or copy code from it.
