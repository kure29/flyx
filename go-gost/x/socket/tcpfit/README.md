# tcpfit engine

Vendored from https://github.com/Kylin010/tcpfit, version 0.5.9.
Upstream Git blob: `b586a250aceef2ddca4286c755a49e83032fc43e`.
MIT license: see LICENSE. Copyright (c) 2026 Kylin010.

Only the final CLI dispatch was removed so the file can be sourced as a library.
FLYX calls the measurement and policer scan functions through bridge.sh; it does
not call upstream installation, updates, telemetry, sysctl tuning, swap, route
window changes, or archive commands. FLYX owns its sysctl backups and persistence.
The bridge overrides queue restoration to replay a saved configuration, fails
closed for external shapers, and never permits an interactive prompt.
