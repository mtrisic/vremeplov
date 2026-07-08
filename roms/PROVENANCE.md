# ROM provenance

The binaries under `bin/` are Galaksija ROM images preserved by the community
in [mejs/galaksija](https://github.com/mejs/galaksija), downloaded at commit
`66062ef8f8ed511fd34113837d365bd783af53f3` (2026-07-06).

| File | Size | Upstream path | SHA-256 |
|---|---|---|---|
| `rom_a_v28.bin` | 4096 | `roms/ROM A/ROM_A_without_ROM_B_init_ver_28.bin` | `a28fdb91262a7612251c8d2b216797414aa36b2f4276d613777b5aca7e35860b` |
| `rom_a_v29.bin` | 4096 | `roms/ROM A/ROM_A_with_ROM_B_init_ver_29.bin` | `d7f03bf65c1f98c9c1226f4e1d1ecd4ac778a2dc4a332568ab0d5fee7ede9042` |
| `rom_b.bin` | 4096 | `roms/ROM B/ROM_B.bin` | `7d93fe474fcc98c06a6d96e533f8935e4e707edf0792d3e9d679ea68404375de` |
| `chrgen_elektronika.bin` | 2048 | `roms/Character Generator ROM/CHRGEN_ELEKTRONIKA_INZENJERING.bin` | `8276c4c9e37e0e7bb8970f75fdb45b7b0b00f9a95da928c61b9dceebfe94c40f` |
| `chrgen_mipro.bin` | 2048 | `roms/Character Generator ROM/CHRGEN_MIPRO.bin` | `9084463cf766f9082e93bfa002888b7acb102b5f29fe282386fb4bacaa4a523c` |

Download URL form:
`https://raw.githubusercontent.com/mejs/galaksija/<commit>/<upstream path>` (URL-encode spaces).

Checksums are also in `SHA256SUMS` (verified by `.devcontainer/post_create.sh`).

## Sample programs (`core/gtp/testdata/`)

The GTP tape images used as parser/loader test fixtures come from the same
repository and commit (`programs/<dir>/<file>`):

| File | Size | Upstream dir | SHA-256 |
|---|---|---|---|
| `hackaday.gtp` | 613 | `programs/hackaday_demo` | `b66ac9d7677226f19168a1bd242b3ece9d6d0644b2e43873da54140fd12cd9b5` |
| `pumpkin.gtp` | 612 | `programs/halloween` | `1cca1d3d52c65ff257b55ba8fd68c781763df530c70bd6a01abef07e55acc90b` |
| `retroinfo.gtp` | 2477 | `programs/retroinfo_demo` | `befb2cc5666f677ab5d7f5c33654ff9ac93c7fd4a71ba1dddce4019711546675` |
| `win11check.gtp` | 782 | `programs/win11check` | `d565fff6c90857fdd55d645391aeba37be5860d37e3cf0f64ffa953eca3cdd87` |

The licensing note below applies to these as well (recent community programs
published in the same open preservation repo without explicit licenses).

Note: `rom_a_v29.bin` unconditionally `CALL`s 0x1000 during boot ("with ROM B
init") and therefore **requires** `rom_b.bin` in the socket; `rom_a_v28.bin`
is the ROM for a machine without ROM B (see AGENTS.md discrepancy log 11).

## Licensing

Neither the mejs/galaksija repository nor the ROM images carry an explicit
license. They are preserved historical material (1983–1984, Elektronika
inženjering / Voja Antonić) hosted openly by the preservation community, in
which the original designer actively participates. They are committed here for
reproducibility on that basis, not under this repository's MIT license. If a
rights holder objects, the images will be removed and replaced with a fetch
script.
