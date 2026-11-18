#!/bin/sh
# Verifies internal/preview/assets against SHA256SUMS. To bump a version:
# download it manually (paths in assets/ASSETS.md), update SHA256SUMS, and
# this script keeps every contributor honest without network at build time.
set -e
cd "$(dirname "$0")/../internal/preview/assets"
sha256sum -c SHA256SUMS
