# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

This repository is a monorepo consisting of:
1. **`new-api`** (Go + React 19 + Rsbuild): Next-generation LLM Gateway and AI Asset Management System (SaaS front & back).
2. **`hermes-agent`** (Python): Self-improving AI agent built by Nous Research.
3. **`hermes-workspace`** (TypeScript/React): Frontend workspace for Hermes.
4. **`savvy-manager`** (Go/Python): Backend control orchestrator for Docker container lifecycle.

## Development & Test Commands

### Go Backend (`new-api`)
- Build backend: `go build` (from `new-api/`)
- Run tests: `go test ./...`
- Run single test: `go test -v ./service -run TestSomeFunction`

### Frontend (`new-api/web/default`)
- Install deps: `bun install`
- Dev server: `bun run dev`
- Build: `bun run build`
- Sync translations: `bun run i18n:sync`

### Python AI Agent (`hermes-agent`)
- Install deps: `uv pip install -e ".[all,dev]"`
- Run tests: `pytest`
- Run single test: `pytest tests/test_file.py -k "test_method"`

### Savvy Manager (`savvy-manager`)
- Python tests: `python -m pytest tests -q`

## High-Level Architecture

### `new-api` (Gateway & User Console)
- Layered structure: `router/` -> `controller/` -> `service/` -> `model/` (GORM).
- Special attention is needed for database compatibility. Code must support SQLite, MySQL (>=5.7.8), and PostgreSQL (>=9.6). Use Dialect adapters and helpers in `model/main.go`.
- All JSON marshalling/unmarshalling operations MUST go through the custom wrappers in `common/json.go`. Direct use of `encoding/json` is prohibited.
- `relay/` contains the AI provider adapters and routing.

### `savvy-manager` (Docker Lifecycle Orchestration)
- Manages instance records, Docker container launch, and 2-hour free session auto-sleeping.
- Does NOT share DB with `new-api` and communicates via signed HMAC private APIs.

## Project Governance & Brand Rules
- **Protected Project Information**: NEVER modify, delete, or replace references, mentions, branding, or attribution related to:
  - **`new-api`** (the project name/identity)
  - **`QuantumNous`** (the organization/author identity)
- Public Brand for the SaaS is **Savvy Agent**; the hosted container product name is **Hermes Cloud Workspace**.
- Company is **郑州市管城回族区栗橙网络科技工作室(个体工商户)** (工商执照登记全称; 简称/对外品牌用 **栗橙科技**); support email is `support@scheng.net`.
  - ⚠️ 凭证/链证/备案类权威表述一律写工商执照全称"郑州市管城回族区栗橙网络科技工作室(个体工商户)", 禁用旧写"粟城科技网络工作室"(2026-07-20 核正)。
- Free plan workspace containers run for 2 hours per start, then automatically sleep via manager daemon. Data/volumes are preserved. (User-corrected 2026-07-09; previously listed as 3 hours.)
