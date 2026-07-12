# Assembly debugging example

A tiny hand-assembled Z80 program to try `vremeplov-dap` (source-level
debugging in your editor) without installing an assembler: `hello.asm`
is the source, `hello.bin` the bytes (org `0x8000`), `hello.sld` the
sjasmplus-style source map. The same files serve as `cmd/dap` test
fixtures (kept as separate copies so example churn cannot break tests).

VS Code `launch.json`:

```json
{
  "type": "galaksija",
  "request": "launch",
  "name": "Debug hello.asm",
  "program": "${workspaceFolder}/examples/asm/hello.bin",
  "org": "0x8000",
  "sld": "${workspaceFolder}/examples/asm/hello.sld",
  "screen": "127.0.0.1:8390"
}
```

Set a breakpoint on the `LD (0x9000),A` line, F5, then step — in both
directions.

For real projects, assemble with [sjasmplus](https://github.com/z00m128/sjasmplus):

```sh
sjasmplus prog.asm --raw=prog.bin --sld=prog.sld --fullpath
```

The README's "Debugging a Galaksija program" section has the full
editor setup (VS Code and Helix).
