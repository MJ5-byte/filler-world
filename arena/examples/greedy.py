#!/usr/bin/env python3
"""Example Filler bot: picks the valid placement closest to the enemy.

Protocol (stdin/stdout):
  $$$ exec p1 : [path]        <- once, tells us which player we are
  Anfield W H:                <- each turn: board with ruler + numbered rows
  Piece W H:                  <- the piece to place
  -> print "X Y" (top-left of the piece), or "0 0" if nothing fits
"""
import sys


def read_line():
    line = sys.stdin.readline()
    if not line:
        sys.exit(0)
    return line.rstrip("\n")


def main():
    first = read_line()
    me = ("@", "a") if "p1" in first else ("$", "s")
    enemy = ("$", "s") if "p1" in first else ("@", "a")

    while True:
        line = read_line()
        while not line.startswith("Anfield"):
            line = read_line()
        h = int(line.split()[2].rstrip(":"))
        read_line()  # column ruler
        grid = [read_line()[4:] for _ in range(h)]
        w = len(grid[0])

        line = read_line()
        while not line.startswith("Piece"):
            line = read_line()
        ph = int(line.split()[2].rstrip(":"))
        piece_rows = [read_line() for _ in range(ph)]
        cells = [(x, y) for y, row in enumerate(piece_rows)
                 for x, c in enumerate(row) if c != "."]

        enemy_cells = [(x, y) for y in range(h) for x in range(w)
                       if grid[y][x] in enemy]

        best, best_d = None, None
        for oy in range(h):
            for ox in range(w):
                overlap = 0
                ok = True
                for px, py in cells:
                    x, y = ox + px, oy + py
                    if x >= w or y >= h:
                        ok = False
                        break
                    c = grid[y][x]
                    if c in me:
                        overlap += 1
                        if overlap > 1:
                            ok = False
                            break
                    elif c in enemy:
                        ok = False
                        break
                if not ok or overlap != 1:
                    continue
                # distance of the placement to the nearest enemy cell
                d = min((abs(ox + px - ex) + abs(oy + py - ey)
                         for px, py in cells for ex, ey in enemy_cells),
                        default=0)
                if best_d is None or d < best_d:
                    best, best_d = (ox, oy), d

        if best:
            print(f"{best[0]} {best[1]}", flush=True)
        else:
            print("0 0", flush=True)


if __name__ == "__main__":
    main()
