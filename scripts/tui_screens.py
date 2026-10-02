#!/usr/bin/env python3
"""Drive clical in a pseudo-terminal and print what the screen actually shows.

Bubble Tea only redraws the cells that change, so grepping raw terminal output
misses most updates (going from "Week 40" to "Week 41" sends just "1"). This
replays the output through a terminal emulator (pyte) and prints full screens.

Setup (once, outside the repo):
    python3 -m venv /tmp/clical-venv && /tmp/clical-venv/bin/pip install pyte

Usage — always with a throwaway data directory, never the real vault:
    go build -o /tmp/clical ./cmd/clical
    XDG_DATA_HOME=/tmp/clical-data /tmp/clical-venv/bin/python scripts/tui_screens.py \\
        --size 110x24 -- /tmp/clical \\
        wait:passphrase 'keys:pw\\r' wait:Synced keys:2 show:week 'keys:]]' show:'two weeks on' keys:q

Steps:
    wait:TEXT   wait (up to 20s) until TEXT is on screen
    keys:KEYS   type KEYS, then let the screen settle for 0.15s; backslash
                escapes work (\\r enter, \\t tab, \\x1b esc)
    sleep:SECS  keep reading output for SECS seconds
    show:LABEL  print the current screen under LABEL
"""

import fcntl
import os
import pty
import select
import struct
import sys
import termios
import time

try:
    import pyte
except ImportError:
    sys.exit("pyte is required: python3 -m venv /tmp/clical-venv && /tmp/clical-venv/bin/pip install pyte")


def main() -> int:
    args = sys.argv[1:]
    size = "100x24"
    if args[:1] == ["--size"]:
        size, args = args[1], args[2:]
    if args[:1] == ["--"]:
        args = args[1:]
    argv = [a for a in args if not _is_step(a)]
    steps = [a for a in args if _is_step(a)]
    if not argv:
        sys.exit(__doc__)
    cols, rows = (int(n) for n in size.split("x"))

    pid, fd = pty.fork()
    if pid == 0:
        os.execvp(argv[0], argv)
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
    screen = pyte.Screen(cols, rows)
    stream = pyte.ByteStream(screen)

    def pump(seconds: float) -> None:
        end = time.time() + seconds
        while time.time() < end:
            ready, _, _ = select.select([fd], [], [], 0.05)
            if ready:
                try:
                    data = os.read(fd, 65536)
                except OSError:
                    return
                if not data:
                    return
                stream.feed(data)

    failed = False
    for step in steps:
        kind, _, arg = step.partition(":")
        if kind == "wait":
            end = time.time() + 20
            while time.time() < end and not any(arg in line for line in screen.display):
                pump(0.1)
            if not any(arg in line for line in screen.display):
                print(f"[timeout waiting for {arg!r}]")
                failed = True
                break
        elif kind == "keys":
            os.write(fd, arg.encode().decode("unicode_escape").encode())
            pump(0.15)
        elif kind == "sleep":
            pump(float(arg))
        elif kind == "show":
            print(f"===== {arg}")
            for line in screen.display:
                print(line.rstrip())

    pump(1.0)
    done, status = os.waitpid(pid, os.WNOHANG)
    if done == 0:
        print("[still running after the last step; terminated]")
        os.kill(pid, 15)
        _, status = os.waitpid(pid, 0)
        return 1
    code = os.waitstatus_to_exitcode(status)
    print(f"[exit {code}]")
    return 1 if failed or code != 0 else 0


def _is_step(arg: str) -> bool:
    return arg.split(":", 1)[0] in ("wait", "keys", "sleep", "show") and ":" in arg


if __name__ == "__main__":
    sys.exit(main())
