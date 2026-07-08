#!/usr/bin/env python3
"""Headless smoke test for the TUI: runs it in a pty, answers the
terminal handshake bubbletea performs, types PRINT 3*7 <Enter> into
Galaksija BASIC, then quits via Ctrl+X q and asserts that the captured
screen contains sub-cell graphics runes and a live status line.

Usage: python3 smoke.py <path-to-tui-binary>
"""
import fcntl
import os
import pty
import re
import struct
import sys
import termios
import time

ROWS, COLS = 60, 140


def main():
    binary = sys.argv[1]
    pid, fd = pty.fork()
    if pid == 0:
        os.environ["TERM"] = "xterm-256color"
        os.execv(binary, [binary])
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))

    captured = bytearray()
    deadline = time.time() + 12
    typed = False
    quit_sent = False

    def send(data: bytes):
        os.write(fd, data)

    while time.time() < deadline:
        try:
            chunk = os.read(fd, 65536)
        except OSError:
            break
        if not chunk:
            break
        captured.extend(chunk)
        # Answer cursor-position queries (ESC[6n) and OSC color queries.
        for _ in range(chunk.count(b"\x1b[6n")):
            send(b"\x1b[%d;1R" % ROWS)
        if b"\x1b]11;?" in chunk:
            send(b"\x1b]11;rgb:0000/0000/0000\x1b\\")
        if b"\x1b]10;?" in chunk:
            send(b"\x1b]10;rgb:ffff/ffff/ffff\x1b\\")

        text = captured.decode("utf-8", "replace")
        if not typed and "vremeplov" in text:
            time.sleep(2.5)  # let the machine boot to READY
            for ch in b"PRINT 3*7\r":
                send(bytes([ch]))
                time.sleep(0.15)
            time.sleep(1.5)
            typed = True
        elif typed and not quit_sent:
            send(b"\x18")  # Ctrl+X
            time.sleep(0.3)
            send(b"q")
            time.sleep(0.3)
            send(b"y")  # confirm the quit prompt
            quit_sent = True

    os.close(fd)
    os.waitpid(pid, 0)

    text = captured.decode("utf-8", "replace")
    subcell = sum(
        1
        for ch in text
        if 0x2800 <= ord(ch) < 0x2900 or ch in "▀▄█▖▗▘▝▚▞▙▛▜▟▌▐"
    )
    status = re.findall(r"vremeplov · [a-z-]+", text)
    print(f"captured {len(captured)} bytes, {subcell} sub-cell runes, "
          f"status lines: {status[:1]}")
    if not typed or not quit_sent:
        print("FAIL: never reached interactive state", file=sys.stderr)
        return 1
    if subcell < 100:
        print("FAIL: no rendered graphics in capture", file=sys.stderr)
        return 1
    if not status:
        print("FAIL: status line missing", file=sys.stderr)
        return 1
    print("TUI-SMOKE-OK")
    return 0


if __name__ == "__main__":
    sys.exit(main())
