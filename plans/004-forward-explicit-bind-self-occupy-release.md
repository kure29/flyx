# 004 Forward Explicit Bind Self-Occupy Release

## Checklist

- [x] Confirm current forward edit/save failure path and lock strategy: explicit bind always stays explicit.
- [x] Add repository query to detect whether a node+port is occupied by other forwards (excluding current forward).
- [x] Enhance forward service sync to treat address-in-use as a recoverable case when only self occupies the port.
- [x] On self-occupy conflict, proactively delete current forward services on target node and retry AddService.
- [x] Keep hard failure when the same node+port is occupied by other forwards.
- [x] Add focused unit tests for new error classification helpers.
- [x] Run focused backend tests for touched handler/repo packages.
