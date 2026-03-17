# OpenClaw Cortex Admin

A standalone Next.js 15 admin dashboard for browsing and managing OpenClaw Cortex memories, entities, and conflict resolution.

## Features

- **Dashboard** — memory collection stats (totals, by type, by scope)
- **Memories** — list, search, filter, and view individual memories with full metadata
- **Entities** — browse extracted entities and their relationships
- **Conflicts** — review and resolve detected memory contradictions
- **Settings** — configure the cortex API endpoint and auth token

## Getting Started

Copy the example environment file and fill in your cortex API details:

```bash
cp .env.local.example .env.local
```

Edit `.env.local`:

```
NEXT_PUBLIC_CORTEX_URL=http://localhost:8080
NEXT_PUBLIC_CORTEX_TOKEN=your-api-token
```

Run the development server (port 3001):

```bash
pnpm dev
```

Open [http://localhost:3001](http://localhost:3001) in your browser.

## Tech Stack

- Next.js 15 (App Router)
- TypeScript
- Tailwind CSS v4
- shadcn/ui (zinc palette, dark mode)
- Geist font
- SWR for data fetching

## Notes

This is a local developer tool — it has no authentication layer of its own. Keep the cortex API token in `.env.local` only and never commit it.
