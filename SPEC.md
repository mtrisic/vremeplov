# Vremeplov — Specification

Vremeplov is a cycle-accurate emulator of the **Galaksija**, the Yugoslav Z80A home
computer designed by Voja Antonić (1983), written in Go on top of the author's own
cycle-accurate Z80 core, **gozilog** (`github.com/mtrisic/gozilog`).

This document is the single source of truth for architecture and hardware facts.
`PLAN.md` records the completed build and the optional future work; `AGENTS.md`
holds working rules for agents and the hardware discrepancy log. A fresh session
must be able to work on this repo from these three documents alone — no chat
history required.

---

## 1. Goals and non-goals

**Goals**

- Cycle-accurate video from day one: the Galaksija generates video with the CPU
  itself (R-register refresh addressing), and Vremeplov reproduces that via
  gozilog's per-T-state `Ticker` hook. Software video tricks and demos must work.
- A portable, embeddable, zero-dependency core usable from native binaries, WASM
  pages, and future frontends.
- Deterministic execution: same ROMs + same (timestamped) inputs ⇒ byte-identical
  results. All automated tests rely on this.
- Headless operation from day zero as the primary test harness.
- Frontends: headless runner, interactive terminal (bubbletea), web
  (WASM/canvas), desktop GUI (Ebiten).

**Non-goals (optional future work — see PLAN.md)**

- A fast character-buffer renderer (must not shape the architecture).

## 2. Fixed decisions

| Topic | Decision |
|---|---|
| CPU core | gozilog, consumed as a tagged Go module (`v1.0.0+`); co-development via an uncommitted local `go work edit -replace` (AGENTS.md) |
| RAM | Configurable 2/4/6 KB (default **6 KB**), plus a non-historical "expanded" option: flat RAM `0x4000–0xFFFF` |
| ROM A | Default **v28** (`ROM_A_without_ROM_B_init_ver_28.bin`) — the historical ROM-A-only machine. v29 (`ROM_A_with_ROM_B_init_ver_29.bin`) is available but **requires ROM B**: it unconditionally `CALL`s 0x1000 at boot (AGENTS.md log 11) |
| ROM B | Optional (`--rom-b`, pairs with ROM A v29); default off; golden tests run ROM A v28 alone |
| Chargen | `CHRGEN_ELEKTRONIKA_INZENJERING.bin` (2048 B) default; Mipro variant selectable |
| ROM distribution | Binaries **committed** to the repo under `roms/` with `PROVENANCE.md` (source URLs, mejs commit SHA, SHA-256 sums, licensing note: no license is attached upstream; preserved historical material) |
| Framebuffer | Full **384×320** frame in core; core exposes the standard 256×208 active rect; frontends crop by default |
| TUI key holds | Timed hold (~3 frames) by default, auto-extended by terminal auto-repeat; sticky mode toggle for games |
| TUI key mapping | See §6.2 |
| WASM scope | Canvas + local `.gtp` file picker (reset→boot→load→RUN) + reload/pause/reset buttons; nothing more |
| License | MIT. Repo `github.com/mtrisic/vremeplov` |
| CI/hosting | GitHub Actions: release binaries on `v*` tags, GitHub Pages (landing page + web emulator) on pushes to `main` |
| Go | Target Go 1.26 toolchain; `go 1.24` minimum in go.mod files |

## 3. Hardware reference

Primary sources: Tomaž Šolc's Galaksija articles and diploma thesis, and his
commented ROM A disassembly (per-instruction T-state annotations of the video
driver). tablix.org is intermittently unreachable — archive.org fallbacks in §9.
Cross-checked against mejs/galaksija materials and MAME. When sources disagree,
prefer measured/documented sources and record the discrepancy in `AGENTS.md`.

### 3.1 Memory map

The peripheral block `0x2000–0x203F` is incompletely decoded and **mirrors every
0x40 bytes** through `0x27FF` (32 copies).

| Range | Contents |
|---|---|
| `0x0000–0x0FFF` | ROM A, 4 KB. Writes ignored. |
| `0x1000–0x1FFF` | ROM B socket, 4 KB. Reads `0xFF` when absent (assumption; see AGENTS.md log). Writes ignored. |
| `0x2000–0x2037` (mirrored) | Keyboard: **one address per key**, state on **D0, active-low** (0 = pressed). Offset `0x00` = tape comparator input. Offsets `0x36–0x37` unused (read 1). Other data bits undefined → return 1s (`0xFE`/`0xFF`). Writes ignored. |
| `0x2038–0x203F` (mirrored) | **Write-only latch.** Decode: any address matching `0010 0xxx xx11 1xxx` (lowest `0x2038`, highest `0x27FF`; ROM uses `0x207F`, `0x2038`, `0x27FF`). Reads undefined → return `0xFF` (assumption; logged). |
| `0x2800–0x3FFF` | Static RAM: 2/4/6 KB ⇒ top `0x2FFF`/`0x37FF`/`0x3FFF`. ROM A auto-sizes RAM at boot (write-and-verify loop from `0x2800`; result+1 into RAM_TOP `0x2A6A`). |
| `0x4000–0xFFFF` | Unused, reads `0xFF` — or flat RAM in the "expanded" config. |

RAM layout used by ROM A: `0x2800–0x29FF` video framebuffer (32×16 character
codes); `0x2A00–0x2C39` system variables (RAM_TOP `0x2A6A`, KEY_DIFF `0x2AA5`,
HORIZ_POS `0x2BA8` (default 11), CLOCK_ON `0x2BAF`, SCROLL_CNT `0x2BB0`, LAST_KEY
`0x2BB4`, BASIC_START ptr `0x2C36`, BASIC_END ptr `0x2C38`); BASIC user program
from `0x2C3A`.

### 3.2 The latch

Write-only 8-bit latch:

| Bit | Function |
|---|---|
| 7 | `A7CLMP` — when **0** (clamp enabled), RAM address line A7 is **forced to 1**; when 1, clamp disabled. Compensates for the Z80's 7-bit R increment so refresh addresses reach the upper 128-byte half of each 256-byte video page. **The clamp acts on the RAM chip's address line, so while enabled it affects ALL RAM accesses (CPU reads/writes included), not just refresh-time video fetches.** Stock ROM only enables it while executing from ROM, so ordinary software never observes it — but the core must model it in the RAM address decode globally. |
| 6 | Cassette output bit 0 |
| 5–2 | `CHR3..CHR0` — character-generator row select (bit 2 = row bit 0) |
| 2 | (doubles as) cassette output bit 1 |
| 1–0 | Unused |

Cassette output is 3-level: both bits 1 → high (~1.0 V), both 0 → low (0 V),
mixed → middle (~0.5 V). The idle latch value `0xBC` (clamp off, row 15,
bit6=0/bit2=1 = mixed) rests the tape line at the middle level; see §3.6 for
the exact SAVE pulse values.

### 3.3 Video generation

There is no video chip. All video is generated by the CPU inside the IM1 interrupt
service routine at `0x0038`:

- During the visible portion of a scanline the ROM executes only 4-T-state
  M1-only opcodes. In the refresh half of each M1 cycle the Z80 drives
  `I<<8 | R` on the address bus; the video RAM chip is selected and puts the
  **character code on the data bus**.
- The 2 KB **chargen ROM** is addressed as `{CHR3..CHR0 row (4 bits, from latch)}
  : {character code (7 bits, from the data bus)}`. Only 7 data lines are wired,
  and inspection of the actual ROM image shows the unconnected line is **D6**:
  glyph index = `(code & 0x3F) | ((code & 0x80) >> 1)` (letters `@A–Z[\]^_` at
  glyph 0x00–0x1F, ASCII `0x20–0x3F` at glyph 0x20–0x3F, 64 pseudo-graphic
  glyphs at 0x40–0x7F selected by code bit 7). File layout: `row<<7 | glyph`,
  16 rows × 8-pixel bytes per glyph; rows 0 and 15 are all-dark in every glyph.
- The chargen output byte loads a 74LS166 **shift register** at the end of every
  M1 cycle, clocked at the 6.144 MHz pixel clock = **2 pixels per T-state**,
  **LSB = leftmost pixel** (empirical: glyphs mirror otherwise; the emulator
  shifts right and fills from the MSB side). The serial fill is logic 1 =
  **black**: during non-M1 cycles the register drains to black, so nothing is
  drawn outside the driver's controlled code. Pixel polarity: chargen bit
  **1 = dark, 0 = bright** (confirmed: blank rows 0/15 are 0xFF).
- Geometry: **32×16 characters**; each character row is **13 scanlines** (chargen
  rows 1–12 drawn + 1 blank bookkeeping line) ⇒ **208 visible lines**. Chargen
  row 15 is blank in every glyph and is latched whenever user code runs (value
  `0xBC`), blanking borders/retrace; row 0 is latched at the end of each scanline.
- ISR structure (`0x0038`): push AF/BC/DE/HL (113 T header); vertical-position
  delay `24 + 18*B` T from SCROLL_CNT; per-row horizontal-position delay from
  HORIZ_POS (default 11, "must be ≥ 2"); then per character row: set
  `I = 0x28 | ((row>>3)&1)`, latch `D` (row-1 value `0x04`/`0x84`, advancing +4
  per scanline; D bit7 = A7 clamp control — clamp enabled for character rows
  4–7 and 12–15), compute R; run 12 drawn scanlines re-reading the same 32 video
  RAM bytes with chargen rows 1–12. R rewind at each scanline end:
  `ld a,r / sub 0x27 / and 0x7f / ld r,a`. After 192 drawn scanlines: latch
  `0xBC`, update the software clock in Y$, exit via `jp (iy)` hook (IY=`0x00FD`
  default → pop, `ei`, `reti`).
- The one-scanline loop (`0x008B–0x00B7`) is **exactly 192 T-states**:
  `ld (hl),d` (7 T, latches row; its own M1 is the 32nd character fetch) +
  31 × 4-T filler opcodes (which double as the ASCII string "BREAK" and the FP
  constant 1.0) + `ld (hl),a` (7) + `dec b` (4) + `jr z` (7 not taken) +
  `ld a,r` (9) + `sub 0x27` (7) + `and l` (4) + `ld r,a` (9) + `dec e` (4) +
  `jp nz` (10) = 192.
  ⚠ Šolc's per-line annotations contain two timing typos (`ld (hl),a` marked
  10 T, `jp nz` marked 12 T; datasheet values are 7 and 10 and they are what
  make the sum 192). Do not "fix" the emulator to match the annotations.

### 3.4 Timing

| Quantity | Value |
|---|---|
| Pixel clock | 6.144 MHz |
| CPU clock | **3.072 MHz** (pixel/2) |
| Scanline | **192 T-states** = 384 pixel clocks = 62.5 µs (16 kHz — deliberately not PAL's 15.625 kHz) |
| Frame | **320 lines = 61,440 T-states = 20 ms (50 Hz)**, non-interlaced |
| Vertical layout | 56 border + 208 active + 56 border lines |
| Horizontal layout | 64 + 256 + 64 pixel clocks (= 32 + 128 + 32 T) |
| INT | Asserted by the free-running divider chain at the **56th hsync after vsync** (start of the first active line), 50 Hz |
| INT-ack WAIT | On the INT-acknowledge cycle, hardware asserts **WAIT until the next hsync edge**, so the first ISR opcode starts scanline-aligned |
| hsync / vsync pulse | ≈0.8 µs / ≈1.2 ms (sync generated by hardware counters, independent of CPU) |

Frame phase convention for the emulator: **frame T=0 = vsync start**; INT asserts
at `ft = 56*192`; ActiveRect y-origin = line 56. INT pulse width is not documented
— emulator assumption: deassert on acknowledge, with a fallback deassert after 4
scanlines if never acknowledged (constant, adjustable; logged in AGENTS.md).

`FAST`/`SLOW` BASIC commands are `di`/`ei` (ROM `0x000E`/`0x0016`) — the screen
blanks in FAST mode. NMI (`0x0066`) is the "hard break" (di + BASIC reset).

### 3.5 Keyboard

54 keys, memory-mapped one-address-per-key at `0x2000 + code`, D0 active-low:

| Offset(s) | Keys |
|---|---|
| `0x01–0x1A` | A–Z (code + 0x40 = ASCII — deliberate ROM-size trick) |
| `0x1B–0x1E` | Up, Down, Left, Right |
| `0x1F` | Space |
| `0x20–0x29` | Digits 0–9 |
| `0x2A–0x2F` | `; : , = . /` |
| `0x30–0x35` | Return, Break, Repeat, Delete, List, Shift (both physical Shift keys are one matrix line) |
| `0x36–0x37` | Unused (read as released) |
| `0x00` | Tape comparator input (active-low during a pulse). The ROM polls exactly `0x2000` in LOAD; the KEY routine scans `0x2034` down to `0x2001`. |

The ROM debounces with 256 consecutive reads and implements its own key-repeat
(Repeat key) and shift translation (shift tables in ROM; the frontends derive
their char→(shift, code) mapping from them via `core.KeystrokeForRune`).

### 3.6 Tape

- **Output**: latch bits 6 and 2 form the 3-level DAC. SAVE writes `0xFC` (high)
  → **662 T** → `0xB8` (low) → **662 T** → `0xBC` (mid rest); one biphase pulse
  ≈0.43 ms. (Docs said ≈650 T; 662 T is measured from the ROM itself —
  AGENTS.md log 12.) The tape recorder (§4.2) captures exactly this waveform
  back into GTP images.
- **WAV codec** (§4.2): `EncodeWAV` synthesizes the same waveform as 16-bit
  mono PCM (+0.8 FS / −0.8 FS / silence; T-states ↔ samples via the
  3.072 MHz clock); `DecodeWAV` recovers pulses from digitized audio —
  DC-blocked, threshold at 30 % of peak, polarity-agnostic biphase
  detection with per-half duration bounds [140 T (the comparator minimum),
  2200 T], PCM 8/16-bit, mono or averaged multi-channel, ≥ 8 kHz. The
  wide ceiling is deliberate: circulating WAVs are mostly synthesized by
  GTP→WAV tools whose halves run ≈1811 T (archive.org collection) or
  ≈2090 T (MAME gtp_cas) — the edge-triggered ROM reads them fine, and
  2200 stays below half the minimal "1" split (AGENTS.md log 16).
- **Input**: analog comparator drives D0 low at offset `0x00` for the duration of
  a pulse; minimum detected pulse ≈140 T. The emulated deck models the active
  window as the 662 T high half, which the ROM's OLD accepts (roundtrip-tested).
- **Audio** (`audio.go`): the tape-out DAC as a pulled sample stream — the
  cassette-port speaker trick games used on a machine with no sound
  hardware. `EnableAudio` + `RenderAudio(rate)` return the machine's mono
  mix (three DAC levels → −0.25/0/+0.25) covering exactly the machine time
  elapsed since the previous call; deterministic, observational (excluded
  from snapshots, bounded backlog), resynchronizes after a rewind. The
  single mixing point is deliberately Plus-ready: an AY-3-8910 PSG would
  mix into the same call with no frontend changes.
- **Modulation**: pulse-position, measured (AGENTS.md log 12): a "0" bit cell is
  **9377 T** with one pulse at the cell start; a "1" cell is **9423 T** with a
  second pulse at **+4705 T**; every byte (leader included) is followed by an
  extra **13421 T** interbyte pause. Bytes LSB-first; ≈327 bit/s effective.
  The ROM's bit reader waits for a pulse edge (~49 T poll, ~8200 T timeout),
  delays ~4360 T, then majority-samples a ~3150 T window for the second pulse.
- **Native block format** (written by SAVE, read by OLD): 96 leader bytes `0x00`,
  sync `0xA5`, start address (LE), end address (LE, = last+1), data bytes, one
  garbage byte, checksum chosen so the 8-bit sum of everything from `0xA5`
  through the checksum ≡ `0xFF`. Plain `SAVE` dumps `0x2C36`..BASIC_END (the
  BASIC_START/END pointers themselves are on tape).

### 3.7 GTP file format

Authoritative source: MAME `src/lib/formats/gtp_cas.cpp` (BSD-3-Clause) for
standard and name blocks; turbo blocks are undocumented there (see below).
A GTP file is a sequence of blocks, each with a **5-byte header**:

| Byte(s) | Meaning |
|---|---|
| 0 | Block type: `0x00` standard, `0x01` turbo (loaded as standard), `0x10` name |
| 1–2 | Payload size, little-endian |
| 3–4 | Unused (zero) |

Type `0x10` payload = NUL-terminated program name. Type `0x00` payload = the
native tape stream of §3.6 starting at the `0xA5` sync byte (leader is not
stored). Test files in mejs/galaksija: `programs/hackaday_demo/hackaday.gtp`
(613 B), `programs/halloween/pumpkin.gtp`, `programs/retroinfo_demo/retroinfo.gtp`,
`programs/win11check/win11check.gtp`.

**Turbo blocks** (`0x01`) carry the *same payload layout* as standard blocks;
only the recording speed differed on real tape. A source survey (AGENTS.md
log 15) found no waveform definition anywhere: MAME, GALe (galaksija.net),
and z88dk all skip type `0x01`, and the one emulator that consumes them
(pbakota/galaksijaemu, descended from Jevremović's original) fast-loads the
payload exactly like a standard block. The only pulse-level "turbo" in
existence is ROM C's QSAVE/QLOAD — a third-party 4 KB ROM at `0xE000`
(issalig/galaksija `roms/rom_c.asm`) using PWM bit cells (high+low halves of
≈411 T for "1", ≈1323 T for "0", decoded against a ≈784 T threshold,
≈6× standard speed) — but its wire format (a 300-bps header carrying
addresses/name/autostart, a ~4 s pause, then an addressless turbo stream)
does not match the GTP payload, and no stock ROM reads it. Vremeplov
therefore validates and loads turbo blocks exactly like standard ones:
fast-load pokes their sections, and the schedule compiler plays them at
standard speed so ROM A's own loader reads them.

### 3.8 ROM images

From `github.com/mejs/galaksija` (pin the commit SHA in `roms/PROVENANCE.md` at
download time; raw URLs under `raw.githubusercontent.com/mejs/galaksija/<sha>/`):

| File | Size | Path in mejs repo |
|---|---|---|
| ROM A v28 (default, no ROM B) | 4096 B | `roms/ROM A/ROM_A_without_ROM_B_init_ver_28.bin` |
| ROM A v29 (requires ROM B) | 4096 B | `roms/ROM A/ROM_A_with_ROM_B_init_ver_29.bin` |
| ROM B | 4096 B | `roms/ROM B/ROM_B.bin` |
| Chargen (Elektronika inženjering) | 2048 B | `roms/Character Generator ROM/CHRGEN_ELEKTRONIKA_INZENJERING.bin` |
| Chargen (Mipro) | 2048 B | `roms/Character Generator ROM/CHRGEN_MIPRO.bin` |

No license is attached to these binaries upstream; they are preserved historical
material hosted openly by the community (Voja Antonić participates in it). The
provenance note must state this plainly.

## 4. Architecture

### 4.1 Module layout

Go workspace (`go.work`, **committed**, with `go.work.sum`; it carries
no replace directives — gozilog comes from GitHub as a tagged module,
and the intra-repo modules resolve relatively via their own `go.mod`s).
Because the `vremeplov/*` module paths are unpublished until the repo goes up,
builds/`go mod tidy` only work under the workspace — this is by design.

```
core/            module github.com/mtrisic/vremeplov/core      — stdlib only + gozilog
                 (core/loader: the shared tape-image → running-program
                 sequence live frontends use; core + gtp + stdlib only)
roms/            module github.com/mtrisic/vremeplov/roms      — committed ROMs + go:embed accessors
cmd/             module github.com/mtrisic/vremeplov/cmd       — cmd/headless (stdlib + core + roms)
frontends/tui/   module github.com/mtrisic/vremeplov/frontends/tui   — bubbletea
frontends/desktop/ module github.com/mtrisic/vremeplov/frontends/desktop — Ebiten GUI, the repo's only cgo user
frontends/wasm/  module github.com/mtrisic/vremeplov/frontends/wasm  — the only syscall/js user
tools/           small Go tools (check-deps, static server); shell only for orchestration
```

**Purity rule**: `core`'s module graph contains nothing but `gozilog` (itself
stdlib-only). Enforced by `tools/check-deps` (runs `go mod graph` in `core/` and
fails on anything else); part of every phase gate. `core` never imports `roms`
— it takes `[]byte` ROM images in its config. Core's own tests load ROM bytes
from the repo's `roms/bin/` directory via a single `testdata` helper that walks
up from the test's working directory to the `go.work` root (do not scatter
relative `../..` paths).

go:embed cannot cross module boundaries, which is why `roms/` is its own module:
frontends and `cmd` import it for embedded ROMs (`roms.ROMA`, `roms.ROMB`,
`roms.ChargenElektronika`, `roms.ChargenMipro`).

### 4.2 Core design

`core.Machine` implements gozilog's `z80.Bus` **and** `z80.Ticker`. It owns
memory, the latch, the video pipeline, the keyboard matrix, and the tape deck.
Everything is synchronous and single-goroutine; **frontends own the run loop and
real-time pacing** — core never reads the wall clock.

**Bus dispatch** exactly per §3.1, including the 0x40-byte peripheral mirroring
and the **global A7 clamp on RAM addresses** (latch b7==0 ⇒ effective RAM address
`addr | 0x80`) — applied to all RAM accesses, CPU and refresh alike (§3.2).

**Video pipeline** (inside `Tick(addr, data, pins)`):

- The machine keeps its own authoritative T-state counter (incremented once per
  `Tick` call, waits included). Frame phase `ft = counter mod 61440`;
  `line = ft/192`, `linePos = ft%192`. **Never use `cpu.Tstates()` for video
  phase** — it is not restored by `SetState` and diverges after snapshot restore.
- Every T-state the shift-register model emits 2 pixels at the beam position;
  when drained, its serial fill produces black.
- On RFSH T-states (`pins&RFSH != 0`): the video address is the `addr` parameter
  (`I<<8|R`, **pre-increment R** — gozilog follows SST-verified real-hardware
  behaviour, which is exactly what the Galaksija circuit latches). Apply the A7
  clamp; if the address decodes to RAM, read that RAM byte (character code) and
  look up `chargen[chrRow<<7 | glyph(code)]` (see §3.3 for the glyph mapping);
  load the shift register at the end
  of the M1 cycle. The exact sub-T-state load/emit alignment (and therefore the
  ActiveRect x-offset) was **calibrated** against the expected READY screen
  with HORIZ_POS=11 centered; the chosen constants are documented in code
  (`core/video.go`, guarded by the calibration test).
- INT: assert (`cpu.SetINT(true)`) at `ft = 56*192`; deassert on acknowledge or
  after the 4-line fallback. On the ack Tick (`pins == M1|IORQ`), return
  `waits = (192 - linePos) % 192` (the exact off-by-one is part of the same
  calibration). Note: `SetINT` is called from inside the `Tick` callback
  (re-entering a CPU method mid-`Step`) — a documented, test-pinned gozilog
  guarantee since v1.1.1 (AGENTS.md log 8).
- Framebuffer: **384×320, 1 byte/pixel, luminance semantics: 1 = bright, 0 =
  dark**. Chargen bits are inverted on the way in (glyph strokes are chargen
  0-bits → bright... careful: chargen bit 1 = dark ⇒ framebuffer value =
  `1 - chargenBit`; border/drained register = black = 0). Sanity check:
  the space character renders all-dark; glyph strokes render bright.
  Double-buffered; swap at vsync (`ft` wrap). Published via a `FrameSink`
  callback and a pull API (`LastFrame() (*Frame, seq uint64)`). `Frame` carries
  `ActiveRect` (256×208; y ∈ [56, 264), x per calibration, nominal [64, 320)).

**Keyboard**: 56-entry pressed-state array (offsets `0x00–0x37`; `0x00` is the
tape comparator, never a key). Immediate API `PressKey(code)` / `ReleaseKey(code)`
plus a deterministic T-state-stamped event queue (`QueueKey(tstate, code, down)`)
consumed as the machine passes each timestamp — headless tests use only the queue.

**Tape deck**: a `TapePulseSource` (comparator level as a pure function of machine
T-state) attachable to the machine; `core/gtp` parses GTP and compiles type-0x00
blocks into a deterministic pulse schedule using the ROM's own timing constants
(≈650 T half-pulses, §3.6). Attach/rewind/detach.

**Tape recorder** (SAVE capture, `core/tape.go`): `StartTapeRecording()` /
`TapeRecording()` / `StopTapeRecording() [][]byte`. A pulse detector on latch
writes classifies the 3-level output (bits 6/2) and accepts only
high→low→mid sequences with both halves in a band around the measured 662 T —
video-ISR latch traffic never reaches the high level, so it cannot fabricate
pulses. Stop splits the pulse log on silence (> 40000 T), decodes bits
(second pulse < 6000 T after a cell start = "1", LSB-first), strips the zero
leader, and returns one stream per SAVE, each starting at `0xA5` — exactly a
GTP standard-block payload. `gtp.Build(name, streams...)` wraps them (plus an
optional 0x10 name block) into an image `Parse` always accepts. The recorder
is observational and excluded from snapshots (a mid-SAVE snapshot drops the
partial capture).

**WAV codec** (`core/wav.go`, §3.6): `(*TapeSchedule).EncodeWAV(rate)` and
`DecodeWAV(data) (*TapeSchedule, error)` convert schedules to and from
digitized audio; `(*TapeSchedule).TapeStreams()` exposes the recorder's
stream decoding for schedules from any source, so WAV input reaches the
same GTP-payload bytes as a captured SAVE. A decoded WAV plays through
`InsertTape` with its original pulse timing.

**Loaders**: `LoadBinary(addr uint16, data []byte)` (poke); GTP **fast-load**
(decode the 0xA5 stream, poke memory, BASIC pointers land as part of the dump)
as a convenience alongside faithful tape playback; `TypeText(s string)` compiles
a listing into queued key events (hold ~3 frames, gap ~2 frames; pacing tuned
against the ROM debounce, double letters included — see the LAST_KEY note).

**Control**: `Reset()`, `StepInstruction()`, `RunTstates(n)`, `RunFrame()`.
Pause = the frontend stops calling; no goroutines in core.

**Debugger** (`core/debug.go`): instruction breakpoints
(`AddBreakpoint`/`RemoveBreakpoint`/`Breakpoints`) and bus-access
watchpoints (`AddWatch(start, end, kind)` with inclusive ranges and
read/write/rw kinds). `RunDebug(n)` mirrors `RunTstates` but returns a
typed `Stop` (budget / breakpoint / watch with address, direction, data);
resuming from a breakpoint executes the instruction under it first.
Watchpoints hook the bus control pins in `Tick` (`MREQ|RD` / `MREQ|WR`,
M1 excluded), so only the CPU's own data accesses trigger — opcode
fetches, the refresh-time video fetch, and `DumpMemory` never do.
Debugger state is observational, never alters execution (equivalence is
tested), and is excluded from snapshots.

**Rewind history** (`core/history.go`): `EnableHistory(interval, depth)`
keeps a ring of automatic snapshots (taken at instruction boundaries
inside `StepInstruction`; ≈250 KB each, framebuffer-dominated) plus a
journal of every external input between them — immediate key
presses/releases (the one input path snapshots don't capture), queued
key batches, tape deck operations, `Reset`, `LoadBinary`. `RewindTo(t)`
restores the nearest snapshot at or before t and replays the journal
forward: byte-identical to the original run by the determinism
guarantee (gob-equal anchor test). "The machine at boundary T" includes
every input applied at T. `StepBack(n)` replays once to discover
boundary T-states, then lands exactly n instruction boundaries back.
Rewinding discards the abandoned timeline (ring, journal, and recorder
pulses past the target); breakpoints survive. A manual `Restore`
rebases the history, and `HistoryRebase()` covers mutations that bypass
the journaled entry points (`CPU().SetState`). History is
infrastructure around snapshots and is itself excluded from them.

**Snapshot**: versioned struct — `z80.State`, RAM, latch, machine T-state
counter, key matrix, pending key queue, tape attachment + position. `encoding/gob`
for v0. Test: restore + identical subsequent inputs ⇒ byte-identical continuation.

### 4.3 Determinism rules

No wall clock, no goroutines, no map iteration on any execution path in core.
All external inputs enter as T-state-stamped events. Golden frames are PNGs
(stdlib `image/png` encoding is deterministic for identical pixel input).

### 4.4 gozilog integration and co-development

- gozilog API used: `z80.New(bus)`, `Step`, `Run`, `Reset`, `SetINT`, `NMI`,
  `State`/`SetState`, `Ticker.Tick` (per-T-state, wait injection via return
  value — **verified**: waits are sampled on the INT-ack `M1|IORQ` T-state,
  `z80/cpu.go:341`), `Pins` (`M1`, `RFSH`, `MREQ`, `IORQ`, `RD`, `WR`, `HALTP`).
  Galaksija uses IM1, so the optional `IntAcker` is not needed (default `0xFF`
  bus read is correct). The TUI monitor additionally uses `z80/disasm`
  (gozilog ≥ v1.1.0) for its disassembly window and step-over lengths.
- gozilog is consumed as a normal tagged module from
  `github.com/mtrisic/gozilog`. For co-development, point an
  **uncommitted** `go work edit -replace github.com/mtrisic/gozilog=…`
  at a local checkout; nothing replace-related is ever committed.
  After the basic concepts are proven: pin by commit SHA in `core/go.mod`, drop
  the replace and the mount requirement.
- Never hack around or vendor-patch a gozilog gap. Propose a gozilog change,
  **stop and get the user's confirmation before touching the gozilog working
  tree or committing to it**, and build/verify gozilog changes with gozilog's
  own devcontainer. File every gap/friction/bug as an issue on the gozilog
  repo.

## 5. Frontends

### 5.1 `cmd/headless` (day zero, primary test harness)

Flags: `--rom-a`, `--rom-b`, `--chargen elektronika|mipro|<path>` (embedded
defaults from `roms`), `--ram 2|4|6|expanded`, `--frames N` / `--tstates N`,
`--dump-frame out.png` (full frame; `--crop` for active rect),
`--dump-mem out.bin[:start-end]`, `--keys script`, `--load-bin addr:file`,
`--snapshot-save f` / `--snapshot-load f`, `--record-tape out.gtp|out.wav`
(record SAVEs during the run as GTP or audio, by extension; error if
nothing captured), `--tape file.gtp|file.wav` (faithful
playback — WAVs keep their decoded pulse timing; `--turbo` = run the tape
before the frame budget), `--load-gtp
file.gtp` (fast poke-load), `--type file.bas|-` (BASIC listing, stdin with
`-`), `--boot-frames N`.

**Key script format** (stable — golden tests depend on it): text lines
`<tstate> <down|up> <key>`, where `<key>` is a symbolic name (`A`–`Z`, `0`–`9`,
`SEMI COLON COMMA EQUALS DOT SLASH SPACE UP DOWN LEFT RIGHT ENTER BREAK REPEAT
DELETE LIST SHIFT`) or a hex matrix offset (`0x31`). Comments `#`, blank lines
ignored.

### 5.2 `frontends/tui` (bubbletea)

- Renders the active rect (full-frame toggle available). Cell math: half-block
  (1×2 px/cell) needs 256×104 cells; quadrant blocks (2×2) 128×104; braille
  (2×4) 128×52. **Auto-select** the crispest mode that fits the terminal,
  manual override in chrome. 50 Hz pacing with frame skip when the terminal
  can't keep up.
- Machine keys: letters/digits/symbols direct (char→(shift,code) table derived
  from the ROM's shift tables); arrows→arrows; Enter→Return; Esc→Break;
  Backspace→Delete; Tab→List; Ctrl+R→Repeat.
- Key holds: terminals deliver presses only, so a press sets the matrix bit for
  ~3 frames (past the ROM's 256-read debounce) and auto-releases; terminal
  auto-repeat extends the hold. Sticky mode (toggle) keeps the last key held
  until the next event, for games.
- Chrome: a **footer** of clickable buttons (mouse enabled via
  bubbletea `WithMouseCellMotion`) under the status line, each labeled
  with its **Ctrl+X prefix** key (never forwarded; still the
  keyboard path — plain letters must type into the machine): quit
  (two-step: asks for `y`, any other key or button cancels — a stray
  click must not kill a session), pause/resume, reset, memory dump to
  file, sticky toggle, renderer
  switch, full-frame toggle, monitor toggle, tape-record toggle (`t`:
  arm the recorder — `REC` status flag — then stop and write
  `vremeplov-tape-<frameseq>.gtp` plus the same capture as `.wav`
  audio), rewind (`b`: 2 s back per press; two minutes of history by
  default, `--rewind N` seconds configures it, 0 disables), and
  snapshots (`w` writes
  `vremeplov-snap-<frameseq>.gob`, `l` restores the newest such file —
  core's gob format, interchangeable with headless
  `--snapshot-save/--snapshot-load` and the web buttons), and
  screenshots (`c` writes `vremeplov-shot-<frameseq>.png` via core's
  `FramePNG`; active area or full frame per the `f` toggle).
  Clipboard: bracketed paste (one bubbletea `KeyMsg` with `Paste`)
  types through `TypeText`, validated before queueing — no OS
  clipboard access, works over SSH; with the monitor open the first
  pasted line lands on the REPL input. `y` copies `ScreenText()` out
  as an **OSC 52** escape written to the terminal (silently ignored
  where unsupported, e.g. macOS Terminal.app). Buttons are
  color-coded by function on the 16-color ANSI palette (red =
  quit/armed recorder, gray = save states, blue = machine control,
  magenta = time machine, cyan = debugging, green = view/input
  toggles; lipgloss strips styling on terminals without color). Button
  rows wrap to the terminal width; labels track state (pause/resume,
  record/stop-rec…). They grow rounded borders when the terminal is
  tall enough that the extra rows leave the full 52-row braille view
  intact (`footBorderMinContent`), and on terminals too short to keep
  a full 16-row text screen above them they collapse to the status
  line alone (the small-panel fallback keeps priority; `footer.go`).
- **Monitor** (`monitor.go`, ^X m): a machine-language debugger panel beside
  the running screen (screen left, panel right; monitor-only below 76
  columns). Registers + flags, a live disassembly window at PC (gozilog
  `z80/disasm`; `▶` marks PC, `●` a breakpoint), display watches, and a
  command REPL: `c`/`p`, `s [n]`, `bs [n]` (reverse-step via rewind
  history), `rw [frames]` (rewind), `n` (step-over via a temporary breakpoint
  at PC+len, budget-capped), `to`, `frame`, `b`/`bd`/`bl`, `w`/`wd`/`wl`
  (core watchpoints), `x` (hex dump, CPU-eye — the A7 clamp applies,
  AGENTS.md log 14), `d`, `poke`, `watch`/`unwatch`, `set REG V`, `reset`.
  Opening pauses the machine; the frame loop always runs through
  `RunDebug`, so a breakpoint/watch hit pauses and reopens the panel even
  when closed. While open, all keys go to the REPL (close it to type into
  the Galaksija). Addresses/bytes/lengths are hex; counts decimal.

### 5.3 `frontends/wasm`

`GOOS=js GOARCH=wasm`. Canvas rendering (ImageData, integer scaling), keydown/
keyup → matrix (real key-up events — no hold synthesis), `<input type=file>`
for a local `.gtp` (always a clean sequence: reset → boot to READY →
fast-load → RUN; digitized `.wav` audio is RIFF-sniffed, decoded, and
fast-loaded the same way), paste-to-type (clipboard text queues through
`TypeText`, validated before queueing; text fields keep native paste),
a ⏪ rewind button (2 s per click, hold to scrub backwards; 60 s history
ring), plus controls in three labeled groups —
machine (pause/resume, reset, monitor), tape (picker, reload,
record + format selector; recording downloads the captured SAVEs as
`recording-<frameseq>.gtp` or `.wav`), and state (Save downloads a
`vremeplov-snap-<frameseq>.gob` snapshot, Load restores one — core's
gob format, interchangeable with the TUI and headless). A Shot button
downloads the active area as a grayscale PNG via core's `FramePNG` —
byte-identical to headless `--dump-frame --crop`. A Sound toggle
streams core's `RenderAudio` to Web Audio as scheduled buffers (the
AudioContext is created on the click — browsers require a gesture);
transport bursts (tape boot, snapshot jumps) are dropped, not played.
ROMs embedded via the `roms` module. Static `web/` dir (page + `wasm_exec.js` + built wasm);
`tools/serve` is a tiny Go static server for manual testing.

**Monitor**: the *Monitor* button toggles a hidden-by-default debugger
panel (docked beside the canvas on wide screens, wrapping below it on
narrow ones), driven by the shared `core/monitor` engine —
registers, disassembly window at PC, display watches, scrollback, and a
command input (same REPL as the TUI). Opening pauses the machine; the
animation loop runs frames through `RunDebug`, so a breakpoint or
watchpoint hit pauses and opens the panel even while a program runs.
Keyboard routing is focus-based: keys go to the matrix unless a text
input (the REPL prompt, the file picker) has focus — unlike the TUI, the
machine can be typed at while the panel is open. Transport actions
(file pick, reload, reset) burst through `RunTstates` and intentionally
ignore breakpoints.

### 5.4 `frontends/desktop` (Ebiten)

The native GUI, and the repo's only cgo user (linux/macOS; Ebiten needs
no C compiler on Windows, so windows binaries cross-compile from a
linux host — `tools/build-desktop.sh` groups targets by build host).
One emulated frame per `Update` tick (`SetTPS(50)`; Ebiten's internal
clock provides the catch-up policy the TUI and wasm loops hand-roll),
run through `RunDebug` so breakpoints stop the loop.

Rendering: the luminance frame → RGBA → a persistent texture drawn at
the largest integer scale that fits beside the monitor panel and above
the footer (F10 toggles active area ↔ full 384×320 frame, F11
fullscreen). Keyboard: physical keys translate through the US layout to
`core.KeystrokeForRune` (wasm semantics — the machine sees the
character the user meant), with real key-down/up and a per-machine-key
refcount so chords sharing a matrix line (both Shifts, strokes carrying
KeyShift) release cleanly; host Shift+`;` (`:`, unshifted on the
Galaksija) suppresses machine Shift for that hold — a quirk the web
frontend still has. Sound: `RenderAudio(48000)` → mutex-guarded FIFO →
`NewPlayerF32` (float32 stereo, no quantization), on by default (no
gesture rule), silence on underrun, ~200 ms backlog cap, dropped across
transport bursts and rewind resyncs.

Chrome: a clickable footer button bar above the status line (state-
tracking labels, the TUI's color groups, wraps on narrow windows);
every button doubles as an F-key — the Galaksija has none, so they
never shadow the machine. Tape in: drag-and-drop or a positional CLI
argument (`.gtp`/`.wav` through `core/loader`; `.gob` restores a
snapshot); F7 reloads, F8 records (`vremeplov-tape-N.{gtp,wav}`).
F5/F6 exchange `vremeplov-snap-N.gob` with every other frontend, F12
writes `FramePNG`, F4 rewinds 2 s and repeats while held. The window
close button arms a two-step confirmation (`SetWindowClosingHandled`);
machine presses are swallowed while it asks, releases still land.
Clipboard (`atotto/clipboard` — pure Go on Windows so the cgo-free
cross-build survives; needs xclip/xsel on Linux): Cmd/Ctrl+V and
footer *paste* type through `TypeText` (monitor open → REPL input),
Cmd/Ctrl+C and footer *copy* put `ScreenText()` out. Meta chords
never reach the matrix; Ctrl gives up only the V/C chords and remains
REPT otherwise.

**Monitor**: F9 docks the shared `core/monitor` session on the right —
registers, disassembly at PC, watches, log, REPL with history — the
TUI's composition and pause handshake (modal: the REPL owns the
keyboard while open; a breakpoint/watchpoint stop pauses and reopens
the panel).

Tests stay off the Ebiten runtime (no `NewImage`/`RunGame`), but Ebiten
initializes GLFW at package init on linux, so `tools/check-desktop.sh`
runs them under `xvfb-run` (plus `-race`), then builds linux (cgo) and
windows (CGO_ENABLED=0) binaries. The window itself cannot open in the
container; graphical verification happens on the host, like browser
testing for wasm.

### 5.5 `cmd/dap` (editor debugging via the Debug Adapter Protocol)

`vremeplov-dap` (pure Go; `github.com/google/go-dap` types+codec in
the cmd module — core purity untouched) hosts a machine and serves one
DAP session over stdio (default) or `--listen` TCP, which covers
VS Code, Helix, nvim-dap, and friends. One goroutine owns the machine;
request handlers marshal through a command channel, and while running
the engine executes real-time-paced one-frame `RunDebug` slices (pause
latency ≤ 20 ms, screen view live, breakpoint stops classified into
DAP stopped events via the temp-breakpoint bookkeeping).

Launch: a `.gtp`/`.wav` (fast-load + typed RUN, optional entry
breakpoint) or a raw `.bin` (`org` + `entry`, PC set via
`CPU().SetState` + `HistoryRebase` — the monitor's `set` discipline).
Source-level debugging parses sjasmplus `--sld` maps (`sld.go`:
T-records line↔address, L-records labels; pages ignored — unbanked
64 K): verified source breakpoints, source-annotated stack frame and
disassembly, line-granularity stepping, `entry` by label. Deliberate
v1 semantics: step-over classifies the instruction (only CALL/RST/
DJNZ/HALT/block-repeats arm a run-to at PC+len — a blind PC+len
breakpoint would run away on taken jumps); step-out targets the word
at (SP) (the standard 8-bit heuristic); `stepBack` = `StepBack`, and
`reverseContinue` rewinds to the previous recorded stop
(`EnableHistory` at launch; `history:0` disables with clean errors);
the debug console (`evaluate`) is the shared monitor REPL verbatim,
with a synthetic stopped event when a console command moves the
machine; data breakpoints are not advertised (console watchpoints
cover it; hits still stop with reason "data breakpoint"). An optional
HTTP screen view (`/screen.png` at 10 Hz + `/screen.txt`) is announced
as a DAP output event — editor-agnostic by construction. The whole
protocol surface is tested in-process over `net.Pipe` with exact
PC/memory assertions (determinism); fixtures are hand-assembled
(`cmd/dap/testdata`, mirrored in `examples/asm/`).

## 6. Development environment

All development, building, and verification happens **inside the devcontainer**.
A result reported from a host toolchain does not count.

- **Dockerfile**: Linux base with Go 1.26 toolchain and Node LTS (for WASM
  tests), plus the X11/GL/ALSA/Wayland dev headers and `xvfb` the Ebiten
  desktop frontend needs to compile and test headless. Everything installed
  via Dockerfile/`devcontainer.json`/`post_create.sh`
  — hosts are assumed to have only Docker and VSCode.
- **devcontainer.json**: `golang.go` extension; zsh via the `common-utils`
  feature exactly as in the project brief; mounts:
  - `source=devcontainer-zsh-history,target=/commandhistory,type=volume`
- **WASM testing**: `tools/check-wasm.sh` runs `GOOS=js GOARCH=wasm go test`
  for `core` (portability proof) and the wasm module's testable logic under Node
  using `$(go env GOROOT)/lib/wasm/wasm_exec.js`.
- **launch.json**: default F5 = "TUI (READY prompt)" with
  `"console": "integratedTerminal"` (bubbletea needs a TTY); also "Headless
  boot".
- Headless container usage for agents (both forms work):
  `devcontainer up --workspace-folder . && devcontainer exec --workspace-folder . -- <cmd>`
  or the `docker build`/`docker exec` pattern documented in `AGENTS.md`.

## 7. Testing strategy

- **Golden frames**: committed PNGs under the relevant module's `testdata/`;
  tests render, encode, byte-compare, and on mismatch write the actual next to
  the golden for eyeballing. Regenerating a golden requires a deliberate
  `-update` flag and user-visible diff in review.
- **Text-level assertion**: decode video RAM `0x2800–0x29FF` as 32×16 codes →
  ASCII (`code+0x40` inverse trick for letters) and assert content (e.g.
  "READY") — catches logic errors independently of pixel calibration.
- **Determinism**: run twice, byte-compare dumps; snapshot mid-run, restore,
  byte-compare continuation.
- Never weaken, skip, or special-case tests to make them pass.

## 8. Feature checklist (across all phases)

Load `.gtp` and digitized `.wav` (faithful tape + fast-load) · record SAVEs
back to `.gtp` or `.wav` audio · pipe BASIC listing as typed input · load
raw binary at address · dump memory to file · pause/halt/resume/step ·
interactive TUI at the BASIC READY prompt (clickable footer chrome) ·
monitor/debugger REPL (TUI + web + desktop) with reverse-step · rewind
("time machine": exact, journal-replayed) · snapshot UI (save states in
every frontend, one interchangeable format) · screenshots (`FramePNG`,
every frontend) · sound (cassette-port DAC → `RenderAudio` → Web Audio
and Ebiten audio) · WASM page with
canvas/keyboard/tape picker/recorder · native desktop GUI (Ebiten:
window, drag-and-drop tapes, sound on by default, footer chrome,
docked monitor) · copy-paste (paste-to-type in browser/desktop/TUI;
screen-text copy in desktop/TUI) · headless everything.

## 9. Sources

- Šolc, memory map: `https://www.tablix.org/~avian/blog/archives/2006/09/galaksija_memory_map/`
- Šolc, character generator: `.../2006/09/galaksija_character_generator/`; composite video: `.../2006/08/galaksija_composite_video_generation`
- Šolc, video sync circuit: `.../2006/11/galaksija_video_synchronization_circuit/`
- Šolc, hi-res graphics: `.../2009/01/high_resolution_graphics_on_galaksija`
- Šolc, chargen patch: `.../2017/07/the_galaksija_character_generator_patch/`
- Šolc, commented ROM A disassembly (per-instruction T-states of the video ISR): `https://www.tablix.org/~avian/galaksija/rom/rom1.html`
- Šolc, diploma thesis "Replika mikroračunalnika Galaksija" (2007); English translation: `https://revspace.nl/images/e/e6/Galaksija_project_translation.pdf`
- mejs/galaksija: `https://github.com/mejs/galaksija` (ROMs, schematics, GTP examples, memory-map diagram, translated build instructions)
- MAME GTP format: `https://github.com/mamedev/mame/blob/master/src/lib/formats/gtp_cas.cpp`
- gozilog: local checkout (`/workspaces/gozilog` in-container); `https://github.com/mtrisic/gozilog`

tablix.org is intermittently down — every page above is retrievable via
`https://web.archive.org/web/<url>`.
