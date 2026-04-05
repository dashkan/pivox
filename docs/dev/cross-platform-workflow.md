# Cross-Platform Development Workflow

## Overview

Pivox is developed by a single developer on macOS (Apple Silicon). The native app targets both macOS (SwiftUI) and Windows (WinUI 3). Cross-platform builds are orchestrated over SSH — no file sync tools, no custom servers.

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
  │  SSH to Windows machine
  │
  ▼
Windows machine (SSH)
  │
  │  1. git pull latest
  │  2. Write build spec
  │  3. Launch Claude Code unattended
  │     (--dangerously-skip-permissions --print)
  │  4. Claude builds, tests, fixes, iterates
  │  5. Commits results, pushes
  │
  ▼
macOS receives push notification
Developer reviews commits, merges
```

## Components

### Git as the Sync Mechanism

No file sync. The git repo is the single source of truth:

- macOS pushes shared code + SwiftUI changes
- Windows pulls before starting work
- Windows Claude commits and pushes results
- Developer reviews and merges from macOS

### SSH Commands

Everything runs over SSH. No custom server to build or maintain.

**Build:**

```bash
ssh win 'cd ~/Projects/pivox && git pull && claude --dangerously-skip-permissions --print -p @BUILD_SPEC.md 2>&1 | tee ~/pivox-builds/latest.log'
```

**Check status:**

```bash
ssh win 'pgrep -f "claude.*pivox" && echo "build running" || echo "idle"'
```

**Run tests:**

```bash
ssh win 'cd ~/Projects/pivox && cmake --build build --target test'
```

**Screenshot:**

```bash
ssh win 'powershell -c "Add-Type -AssemblyName System.Windows.Forms; [System.Windows.Forms.Screen]::PrimaryScreen | ForEach-Object { $bmp = New-Object System.Drawing.Bitmap($_.Bounds.Width, $_.Bounds.Height); [System.Drawing.Graphics]::FromImage($bmp).CopyFromScreen($_.Bounds.Location, [System.Drawing.Point]::Empty, $_.Bounds.Size); $bmp.Save(\"$HOME/screenshot.png\") }"' && scp win:~/screenshot.png /tmp/screenshot.png
```

### Claude Code on Windows

Runs via `--dangerously-skip-permissions` for unattended execution. Safe because:

- The build spec constrains scope ("only modify files under `platform/windows/` and shared interfaces")
- Results are committed and pushed — developer reviews before merging
- The machine is a build box, not production. No secrets, no deployments.

`--print` streams Claude's output to stdout, which `tee` captures to a log file.

### Visibility

On macOS, a terminal on a second monitor provides live visibility:

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
4. **SSH to Windows** — run Claude Code with the build spec:
   ```bash
   ssh win 'cd ~/Projects/pivox && git pull && claude --dangerously-skip-permissions --print -p "$(cat)" 2>&1 | tee ~/pivox-builds/latest.log' <<< "$BUILD_SPEC"
   ```
5. **Continue working** — the developer keeps working on macOS. The SSH command runs in the background.

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
- SSH enabled (OpenSSH server)
- RDP/Parsec available for visual debugging when needed

### Network

- Both machines on the same local network
- SSH access configured (`~/.ssh/config` alias `win`)
- Git remote (GitHub) as the shared repository

## Scaling Beyond Two Platforms

The same pattern extends to additional build targets:

| Machine | SSH alias | Purpose |
|---|---|---|
| Windows x64 | `win` | WinUI 3 native app builds |
| Linux (headless) | `linux` | Engine builds, CI validation |
| Windows Server | `winserver` | Engine + AJA driver testing |

The `/deploy-windows` skill becomes `/deploy <target>` with per-target build specs:

```
Developer + Claude (macOS)
  │
  ├──► ssh win → native app build
  ├──► ssh linux → engine build + test
  └──► local → SwiftUI + shared code
       (all in parallel)
```

## Workflow Summary

### Daily Development

1. Write shared C++ and SwiftUI code on macOS
2. Build and test on macOS (fast local iteration)
3. When feature is complete and working on macOS:
   - `/deploy-windows` — triggers Windows build via SSH
   - Keep working on next task while Windows builds
   - Review Windows results when notified
   - Merge if good, iterate if not

### Debugging Windows Issues

For Windows-specific bugs that need interactive debugging:

1. RDP/Parsec into the Windows machine
2. Open the project in Visual Studio
3. Set breakpoints, run debugger
4. Fix locally on Windows, commit, push
5. Pull the fix on macOS, continue

This should be infrequent — most Windows work is straightforward UI implementation from a detailed spec.

### CI Integration

GitHub Actions provides a safety net:

- Every push triggers builds on both macOS and Windows runners
- Catches build breaks before they reach the SSH workflow
- Runs tests on both platforms
- The SSH workflow is for development iteration; CI is for validation
