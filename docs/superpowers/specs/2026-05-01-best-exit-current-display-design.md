# Best Exit Current Selection Display Design

## Goal

When a tunnel uses the `best` multi-exit strategy, show the currently applied best exit in the tunnel list information. Users should be able to see which exit is currently selected without opening logs or diagnosing the tunnel manually.

The display is informational only. It must not change routing, scoring, switching behavior, or the saved tunnel configuration.

## Current Context

- `3.0.0-beta6` adds `best` as a multi-exit strategy.
- Runtime selection is stored in the backend `bestExitManager` in memory, keyed by `TunnelID + OwnerNodeID`.
- Direct multi-entry tunnels make one independent best-exit decision per entry node.
- Tunnels with intermediate chain hops make one independent best-exit decision per final-hop chain node before the exits.
- `tunnelList` and `tunnelGet` currently return `repo.ListTunnels()` output directly, so frontend tunnel data only includes configured exits from the database, not the currently applied runtime choice.
- The frontend tunnel page maps API items in `vite-frontend/src/pages/tunnel.tsx` and renders list information from that data.

## User Decisions

- Show the current best-exit choice in the tunnel list information.
- Use a summary plus detail model for multiple owners.
- Follow the existing tunnel list refresh cadence; do not add polling or a realtime stream in this phase.
- Work text-only; no visual companion is needed.

## Approach

Extend the existing tunnel list/detail response with a lightweight runtime state object for `best` tunnels, then render that state beside the tunnel's exit/strategy information in the existing frontend list UI.

This keeps the display close to the data users already inspect and avoids a separate API or extra frontend request.

## Backend Design

### Response Shape

Add a `bestExitState` object to each tunnel item returned by `tunnelList` and `tunnelGet` when the tunnel has a multi-exit group whose strategy is `best`.

Response shape:

```json
{
  "enabled": true,
  "summary": "香港节点",
  "status": "applied",
  "updatedAt": 1777584000000,
  "reason": "current exit remains best",
  "items": [
    {
      "ownerNodeId": 10,
      "ownerNodeName": "入口 A",
      "ownerRole": "entry",
      "exitNodeId": 30,
      "exitNodeName": "香港节点",
      "updatedAt": 1777584000000,
      "reason": "current exit remains best"
    }
  ]
}
```

If the tunnel is not using `best`, omit `bestExitState` or set it to `null`.

### Owner Semantics

The display must match the routing model:

- If there are no middle chain hops, each entry node is an owner.
- If there are middle chain hops, each node in the final middle-hop group is an owner.

Each owner can have a different current best exit. The UI must not imply that a multi-owner tunnel has one global best exit when the owners differ.

### Summary Rules

- If all owners currently apply the same exit, `summary` is that exit node name.
- If owners apply different exits, `summary` is `多个出口`.
- If no applied decision exists yet, `summary` is `等待探测`.
- If the tunnel has only one exit, `bestExitState` is not needed because there is no dynamic choice.

### State Source

Use the in-memory `bestExitManager` as the source of currently applied decisions.

Add a read-only snapshot method that returns defensive copies of decision state without exposing mutable internal slices. The handler should convert node IDs to display names from the existing tunnel response data first, then fall back to `h.getNodeRecord` only when the current response does not contain the node.

The feature should not persist current choices to the database in this phase. A panel restart may reset the displayed runtime state to `等待探测` until the prober initializes it again from the current saved first exit.

## Frontend Design

Extend the tunnel item type with optional `bestExitState`.

In the tunnel list, only render the current best-exit display when:

- `bestExitState.enabled === true`, or
- the tunnel has an exit group with `strategy === "best"` and the backend returns a waiting state.

Display format:

- Single applied exit: `最优出口：香港节点`
- Multiple applied exits: `最优出口：多个出口`
- Waiting: `最优出口：等待探测`

For multiple owners, render the summary as compact secondary text in the topology/list information cell and set its native `title` attribute to newline-separated detail rows. This avoids adding a new UI dependency or a custom popover. Detail rows should use:

```text
入口 A -> 香港节点
入口 B -> 日本节点
```

For tunnels with middle chain hops, label owners as chain nodes when useful:

```text
中转 M1 -> 香港节点
中转 M2 -> 日本节点
```

Do not add a new periodic refresh. The display updates when the existing tunnel list is refreshed.

## Error Handling

- If the manager has no decision for an owner, show that owner as `等待探测`.
- If an exit node ID no longer exists in the current tunnel response, show `未知出口` for that item and keep the list usable.
- If an owner node ID no longer exists, show `未知入口` or `未知中转` based on the owner role.
- If the backend cannot compute state for one tunnel, omit `bestExitState` for that tunnel and log the error; do not fail the whole tunnel list response.

## Testing

Backend tests:

- `bestExitManager` snapshot returns applied exit IDs without exposing mutable manager state.
- Direct multi-entry `best` tunnel produces one display item per entry owner.
- Middle-hop tunnel produces one display item per final-hop owner.
- Summary is the single exit name when all owners choose the same exit.
- Summary is `多个出口` when owners choose different exits.
- Summary is `等待探测` when no applied decision exists.
- Non-`best` tunnels do not receive `bestExitState`.

Frontend verification:

- Tunnel list renders `最优出口：<name>` for a single applied exit.
- Tunnel list renders `最优出口：多个出口` plus owner details for multiple applied exits.
- Tunnel list renders `最优出口：等待探测` for waiting state.
- `pnpm run build` passes.

Verification commands:

```bash
(cd go-backend && go test ./...)
(cd vite-frontend && pnpm run build)
```

## Non-Goals

- Do not add a new realtime stream or polling loop.
- Do not add a detailed best-exit scoring dashboard.
- Do not persist current best-exit choices to the database.
- Do not change switching thresholds, probing targets, or runtime chain update behavior.
- Do not change existing non-`best` tunnel display behavior.
