# vite-frontend

React dashboard for FLYX. rolldown-vite + TypeScript + Tailwind v4 + shadcn/radix.

## Structure

| Dir/File | Role |
|----------|------|
| `src/App.tsx` | Routes + ProtectedRoute + H5 layout selection |
| `src/main.tsx` | Entry: ReactDOM + BrowserRouter |
| `src/api/` | Axios wrapper, sends raw JWT in `Authorization` |
| `src/pages/` | Route views (forward, node, tunnel, settings, ...) |
| `src/shadcn-bridge/heroui/` | HeroUI-compatible facade — **import from here only** |
| `src/components/ui/` | shadcn/radix primitives |
| `src/styles/globals.css` | Base styles — **must import `tailwind-theme.pcss`** |
| `src/styles/tailwind-theme.pcss` | Tailwind v4 `@theme inline` semantic tokens |
| `vite.config.ts` | host `0.0.0.0:3000`, PWA plugin, `@` alias to `src/` |

## Conventions

- **Auth**: raw JWT in `Authorization` header — no `Bearer` prefix.
- **API envelope**: `{code, msg, data, ts}`, code 0 = success.
- **UI imports**: `src/shadcn-bridge/heroui/*` only, never `@heroui/*` or `@nextui-org/*`.
- **Theme**: don't remove `tailwind-theme.pcss` import from `globals.css` — breaks semantic classes (`bg-primary`, `text-foreground`, `border-input`).

## Anti-patterns

- Don't add `Bearer` prefix to auth header.
- Don't reintroduce `@heroui/*` or `@nextui-org/*` packages.
- Don't add frontend tests (no test infrastructure).
- Don't remove `tailwind-theme.pcss` import.

## Commands

```bash
pnpm install
pnpm run dev           # http://0.0.0.0:3000
pnpm run build         # tsc && vite build
pnpm run lint          # eslint --fix
```
