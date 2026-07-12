; hello.asm — vremeplov-dap test fixture. Hand-assembled: the bytes
; live in hello.bin (org 0x8000), the source map in hello.sld.
        ORG 0x8000

start:  CALL fill
after:  INC A
        JR after

        ORG 0x8008
fill:   LD A,0x2A
        LD (0x9000),A
        RET
