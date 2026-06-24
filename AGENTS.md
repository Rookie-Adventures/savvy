# Savvy — Combined Hermes Project

## Project Structure

This is a **monorepo** that combines two independent projects:

```
savvy/
├── hermes-agent/        # AI agent backend (Python)
│   ├── run_agent.py     # Core agent loop
│   ├── cli.py           # CLI interface
│   ├── gateway/         # Messaging gateway
│   ├── plugins/         # Plugin system
│   ├── skills/          # Built-in skills
│   └── AGENTS.md        # Hermes-agent dev guide
│
└── hermes-workspace/    # Web dashboard & desktop app (TypeScript/React)
    ├── src/             # Frontend source
    ├── electron/        # Electron desktop app
    ├── agents/          # Agent definitions
    ├── swarm.yaml       # Swarm worker config
    └── AGENTS.md        # Workspace agent contract
```

## Git Remote Configuration

| Remote | URL | Purpose |
|--------|-----|---------|
| `origin` | `Rookie-Adventures/savvy.git` | **Your merged repo** — push local changes here |
| `upstream-agent` | `nousresearch/hermes-agent.git` | Pull hermes-agent updates from original project |
| `upstream-workspace` | `outsourc-e/hermes-workspace.git` | Pull hermes-workspace updates from original project |

**Important:** Do NOT use `git push upstream-*` — you don't have write access to the original repos.

## Pulling Updates

```bash
# Update hermes-agent from upstream
git fetch upstream-agent
git pull upstream-agent main -- hermes-agent/

# Update hermes-workspace from upstream
git fetch upstream-workspace
git pull upstream-workspace main -- hermes-workspace/
```

## Pushing Changes

```bash
# Always push to your own repo (origin)
git add .
git commit -m "your message"
git push origin main
```

## Subproject Details

### hermes-agent
- **Language:** Python
- **Upstream:** https://github.com/nousresearch/hermes-agent
- **Purpose:** Personal AI agent with CLI, gateway, plugins, skills
- **See:** `hermes-agent/AGENTS.md` for development guide

### hermes-workspace
- **Language:** TypeScript/React
- **Upstream:** https://github.com/outsourc-e/hermes-workspace
- **Purpose:** Web dashboard, Electron desktop app, swarm management
- **See:** `hermes-workspace/AGENTS.md` for agent contract & swarm config
