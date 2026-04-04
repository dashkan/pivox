# Cross-Platform Development Workflow

## Overview

Pivox is developed by a single developer on macOS (Apple Silicon). The native app targets both macOS (SwiftUI) and Windows (WinUI 3). Rather than file sync tools (Mutagen, NFS) or remote desktop sessions, cross-platform builds are orchestrated via a **remote MCP server** running on the Windows machine.

macOS is the brain. Windows is the executor. Git is the only shared state.

## The Problem

File sync workflows (Mutagen, rsync, NFS/SMB mounts) create friction:

- Sync lag between edit and build
- File conflicts and stale state
- Mental overhead of managing sync state
- Debugging blind on a remote machine

The ideal: develop on macOS, trigger Windows builds with full context, get results back — without thinking about file sync.

## Architecture

```
Developer + Claude Code (macOS)
  │
  │  Develop shared C++ and SwiftUI code
  │  Commit and push to git
  │  Invoke /deploy-windows skill
  │
  ▼
Claude Code generates build spec from changes
  │
  │  MCP tool call over network
  │
  ▼
Windows MCP Server (always running on Windows machine)
  │
  │  1. git fetch + pull latest
  │  2. Create worktree on build branch
  │  3. Launch Claude Code with build spec
  │     (--dangerously-skip-permissions --print)
  │  4. Claude builds, tests, fixes, iterates
  │  5. Commits results to build branch
  │  6. Cleans up worktree
  │  7. Returns summary to macOS
  │
  ▼
macOS Claude receives results
Developer reviews commits, merges
```

## Components

### Git as the Sync Mechanism

No file sync. The git repo is the single source of truth:

- macOS pushes shared code + SwiftUI changes
- Windows MCP pulls before starting work
- Windows Claude commits results to a build branch
- Developer reviews and merges from macOS

### Windows MCP Server

A lightweight MCP server running on the Windows machine, exposed over the local network. It provides tools that Claude Code on macOS can call directly.

**Core tools:**

| Tool | Purpose |
|---|---|
| `build` | Pull latest, run Claude Code with a build spec, return results |
| `status` | Check if a build is in progress, return current state |
| `test` | Run specific tests on Windows, return results |
| `screenshot` | Capture screenshot of the running app for visual verification |

**Implementation sketch:**

```python
# Pseudocode — the MCP server is simple
@tool
def build(spec: str, branch: str = "windows-build") -> BuildResult:
    # 1. Pull latest
    run("git fetch origin && git pull")

    # 2. Create worktree
    worktree_path = create_worktree(branch)

    # 3. Write spec
    write_file(f"{worktree_path}/BUILD_SPEC.md", spec)

    # 4. Run Claude Code unattended
    result = run(
        f"claude --dangerously-skip-permissions --print "
        f"-p @BUILD_SPEC.md",
        cwd=worktree_path,
        stdout=TEE("~/pivox-builds/latest.log")  # visibility
    )

    # 5. Capture results
    commit_hash = get_head_commit(worktree_path)

    # 6. Clean up
    cleanup_worktree(worktree_path)

    # 7. Return
    return BuildResult(
        success=result.exit_code == 0,
        commit=commit_hash,
        log_tail=tail("~/pivox-builds/latest.log", 50)
    )
```

### Claude Code on Windows

Runs via `--dangerously-skip-permissions` for unattended execution. Safe because:

- Always runs in a **git worktree** (isolated from main branch)
- The build spec constrains scope ("only modify files under `platform/windows/` and shared interfaces")
- Results go to a branch, never main — developer reviews and merges
- The machine is a build box, not production. No secrets, no deployments.

`--print` streams Claude's output to stdout, which the MCP server tees to a log file for visibility.

### Visibility

The MCP server tees Claude's output to a log file. On macOS, a terminal on a second monitor provides live visibility:

```bash
ssh win 'tail -f ~/pivox-builds/latest.log'
```

This shows Claude working, build errors, fixes, test results. No interaction — just a window into what's happening on the Windows side.

### The /deploy-windows Skill

A Claude Code skill that orchestrates the full flow:

```
/deploy-windows [description of what to build]
```

When invoked:

1. **Analyze changes** — look at what changed since the last Windows sync (git diff against the last deployed commit)
2. **Generate build spec** — a detailed prompt for the Windows Claude:
   - What shared code changed and why
   - What Windows-specific code needs creating or updating
   - Expected behavior and acceptance criteria
   - Files to modify (scoped to `platform/windows/`, `core/`, `shared-ui/`, `cef/`)
3. **Push to remote** — ensure all changes are pushed
4. **Spawn background agent** — calls the Windows MCP server's `build` tool with the spec
5. **Continue working** — the developer keeps working on macOS. The background agent notifies when the Windows build completes.

### Build Spec Format

The spec is the communication channel. It must be precise enough for unattended Claude to execute without questions:

```markdown
# Windows Build Spec

## Context
Added a new toolbar button for transition preview in the macOS SwiftUI app.
Shared C++ core now has a TransitionPreview class that renders preview frames.

## Changes in shared code (already committed)
- core/preview/transition_preview.h — new class, renders transition frames to a buffer
- core/preview/transition_preview.cpp — implementation using wgpu
- CMakeLists.txt — added core/preview/ to shared lib

## What to build on Windows
1. Add a toolbar button in MainWindow.xaml for transition preview
2. Wire it to TransitionPreview from shared core
3. Host the preview output in a SwapChainPanel in the transition panel
4. Match the macOS behavior: click button → panel opens → shows live preview

## Files to modify
- platform/windows/MainWindow.xaml
- platform/windows/MainWindow.h
- platform/windows/MainWindow.cpp
- platform/windows/MainWindow.idl (if new XAML bindings needed)

## Acceptance criteria
- App builds without errors
- Toolbar button visible and clickable
- Transition preview panel opens and renders
```

## Environment

### macOS (Primary Development Machine)

- Apple Silicon Mac
- Xcode command line tools, CMake, Ninja
- Claude Code as primary development tool
- Full SwiftUI + shared C++ development

### Windows (Build Machine)

- Dedicated Windows 11 x64 machine on local network
- Visual Studio 2026, MSVC, Windows SDK
- Claude Code installed (for unattended execution)
- Windows MCP server running as a service
- SSH enabled (for log tailing and ad-hoc access)
- RDP/Parsec available for visual debugging when needed

### Network

- Both machines on the same local network
- MCP server exposed on a known port (e.g., `http://win:3001/mcp`)
- SSH for log tailing and ad-hoc commands
- Git remote (GitHub) as the shared repository

## Scaling Beyond Two Platforms

The same pattern extends to additional build targets:

| Machine | MCP Server | Purpose |
|---|---|---|
| Windows x64 | `http://win:3001/mcp` | WinUI 3 native app builds |
| Linux (headless) | `http://linux:3001/mcp` | Engine builds, CI validation |
| Windows Server | `http://winserver:3001/mcp` | Engine + AJA driver testing |

Each machine runs its own MCP server. Claude on macOS orchestrates all of them. The `/deploy-windows` skill could become `/deploy <target>` with per-target build specs.

```
Developer + Claude (macOS)
  │
  ├──► Windows MCP → native app build
  ├──► Linux MCP → engine build + test
  └──► macOS local → SwiftUI + shared code
       (all in parallel)
```

## Workflow Summary

### Daily Development

1. Write shared C++ and SwiftUI code on macOS
2. Build and test on macOS (fast local iteration)
3. When feature is complete and working on macOS:
   - `/deploy-windows` — triggers Windows build via MCP
   - Keep working on next task while Windows builds
   - Review Windows results when notified
   - Merge if good, iterate if not

### Debugging Windows Issues

For Windows-specific bugs that need interactive debugging:

1. RDP/Parsec into the Windows machine
2. Open the worktree branch in Visual Studio
3. Set breakpoints, run debugger
4. Fix locally on Windows, commit, push
5. Pull the fix on macOS, continue

This should be infrequent — most Windows work is straightforward UI implementation from a detailed spec.

### CI Integration

GitHub Actions provides a safety net:

- Every push triggers builds on both macOS and Windows runners
- Catches build breaks before they reach the MCP workflow
- Runs tests on both platforms
- The MCP workflow is for development iteration; CI is for validation
