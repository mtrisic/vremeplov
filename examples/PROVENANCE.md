# Example programs — provenance and credits

The `.gtp` tape images in this directory are preserved Galaksija
software, downloaded (2026-07-08) from Tomcat's Yugoslav
retro-computing archive:

<https://retrospec.sgn.net/users/tomcat/yu/Galaksija_screens.php>

Thanks to **tomcat** (tomcat@sgn.net, retrospec.sgn.net) for keeping
this software alive and downloadable. Program authors, as credited on
that page:

| File | Program | Author | Size | SHA-256 |
|---|---|---|---|---|
| `Bioritam1.gtp` | Bioritam 1 | Galaxy Computer | 2996 | `8c887d50e7de3ec25d5912759f1bd7608a5c2a59cd7f324a6d223042744fec16` |
| `Evolucija.gtp` | Evolucija | Dejan Ristanović | 1063 | `c9551509ec8aa66e4c5c9d9fb5ab9023adff6adc2ed31569f46d0ff36a3c127a` |
| `GalaktickiRat.gtp` | Galaktički rat | Voja Antonić | 2238 | `3615cab0d5aabe166c45439dbbaad9b96e2f76f1b8ce6bf032d87fb9d47eb761` |
| `JumpingJack.gtp` | Jumping Jack | Voja Antonić | 1891 | `96c38a5eed3aa28ec254a5ee2a40cdd55ca54a2aa51817eb1dbef9fefb1f07cc` |
| `Monitor.gtp` | Monitor | Voja Antonić | 2073 | `7f9799b47fd451bdea17fad3d4a9a9551cd97f47d82d3e7d11048f02f4e8beff` |
| `Oscilacije.gtp` | Oscilacije | Dragan Vujkov | 504 | `0dae76c570d12a5b590e2fb38b93944966570be72a81bad002ba8c4799524230` |
| `Sintesajzer.gtp` | Sintesajzer | Dragan Vujkov | 901 | `c4720e2e99a92920a19eed6b3ac71f8e8c54b63ebcdb4853f0f7b297ce9d2639` |
| `Zamak.gtp` | Zamak | Voja Antonić | 3015 | `cdc1f57dd683a44456b15df08cb66137a4642c6bf3a9fa88819614c59141ebf9` |

Fittingly, half of these are by **Voja Antonić** — the Galaksija's
designer — and *Evolucija* is by **Dejan Ristanović**; the README's
[Credits and dedication](../README.md#credits-and-dedication) section
says why that matters to this project.

Try one:

```sh
go run ./frontends/desktop examples/JumpingJack.gtp
# or drag-and-drop any of them onto the desktop window / web page
```

Like the ROM images (see [roms/PROVENANCE.md](../roms/PROVENANCE.md)),
these are preserved historical materials with no explicit upstream
license, included here for demonstration and testing. If you hold
rights to any of them and want one removed, open an issue and it goes.

Digitized `.wav` cassette recordings are deliberately **not**
committed (`/examples/*.wav` is gitignored): they are large audio
files carrying the same payloads — the `.gtp` images are the canonical
form, and the emulator can produce a `.wav` from any of them
(`--record-tape`, or `TapeSchedule.EncodeWAV`).
