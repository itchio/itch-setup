#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

echo "Fetching latest locales from itchio/itch-i18n..."
git clone --depth 1 https://github.com/itchio/itch-i18n.git "$TMPDIR/itch-i18n"

echo "Syncing locales into ${ROOT}/data/locales"
mkdir -p "${ROOT}/data/locales"
rsync -av --delete "${TMPDIR}/itch-i18n/locales/" "${ROOT}/data/locales/"

echo "Done."
