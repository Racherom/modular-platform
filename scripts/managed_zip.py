"""Packs deploy/managed into the release zip (spec 0006, child 0009).

Usage: managed_zip.py SOURCE_DIR PREFIX OUT.zip

Every file of SOURCE_DIR except the dev overlay goes under PREFIX/, entries sorted, each with
the fixed 1980 time and mode 0755 or 0644, so the same version always gives the same bytes.
"""

import os
import sys
import zipfile

SKIP = {"compose.dev.yaml", "AGENTS.md", "CLAUDE.md"}


def main(source: str, prefix: str, out: str) -> None:
    paths = []
    for root, dirs, files in os.walk(source):
        dirs.sort()
        for name in files:
            rel = os.path.relpath(os.path.join(root, name), source)
            if name not in SKIP and not rel.startswith(("secrets", "stacks", ".env" + os.sep)) and rel != ".env":
                paths.append(rel)
    with zipfile.ZipFile(out, "w", zipfile.ZIP_DEFLATED) as z:
        for rel in sorted(paths):
            full = os.path.join(source, rel)
            info = zipfile.ZipInfo(f"{prefix}/{rel.replace(os.sep, '/')}", date_time=(1980, 1, 1, 0, 0, 0))
            # git keeps only the executable bit, so modes are normalised to match on any checkout
            mode = 0o755 if os.stat(full).st_mode & 0o100 else 0o644
            info.external_attr = (mode | 0o100000) << 16
            info.compress_type = zipfile.ZIP_DEFLATED
            with open(full, "rb") as f:
                z.writestr(info, f.read())


if __name__ == "__main__":
    main(*sys.argv[1:4])
