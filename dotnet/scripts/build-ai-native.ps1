# build-ai-native.ps1
#
# Builds the two native AI-chat libraries (markdown C++ + highlight
# Rust) and stages the outputs into Pivox.Native/runtimes/<rid>/native/
# where the csproj packs them into the app bundle.
#
# PowerShell port of build-ai-native.sh for Windows.
#
# Usage:
#   pwsh -File dotnet/scripts/build-ai-native.ps1              # release with symbols
#   pwsh -File dotnet/scripts/build-ai-native.ps1 --debug      # debug build
#
# Requires:
#   - cmake, a C++20 compiler (VS 2026 installed)
#   - cargo + rustup
#   - vcpkg (for cmark-gfm); set VCPKG_ROOT env var or install at $HOME/.vcpkg

param(
    [switch]$Debug,
    [switch]$Help
)

$ErrorActionPreference = 'Stop'

if ($Help) {
    Get-Content $PSCommandPath | Select-String '^#' | ForEach-Object { $_.Line -replace '^# ?' } | Select-Object -First 20
    exit 0
}

# ---- config --------------------------------------------------------------

if ($Debug) {
    $BuildType = 'Debug'
    $CargoProfile = 'dev'
    $CargoProfileDir = 'debug'
} else {
    $BuildType = 'RelWithDebInfo'
    $CargoProfile = 'release'
    $CargoProfileDir = 'release'
}

# ---- paths ----------------------------------------------------------------

$ScriptDir = Split-Path -Parent $PSCommandPath
$DotnetDir = Split-Path -Parent $ScriptDir
$MarkdownSrc = Join-Path $DotnetDir 'native' 'markdown'
$HighlightSrc = Join-Path $DotnetDir 'native' 'highlight'
$NativeProj = Join-Path $DotnetDir 'Pivox.Native'

# ---- RID ------------------------------------------------------------------

$Arch = if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture -eq 'Arm64') { 'arm64' } else { 'x64' }
$RID = "win-$Arch"
$StageDir = Join-Path $NativeProj 'runtimes' $RID 'native'
New-Item -ItemType Directory -Path $StageDir -Force | Out-Null

Write-Host "==> Host RID: $RID"
Write-Host "==> Build:    $BuildType / cargo $CargoProfile"
Write-Host "==> Stage:    $StageDir"

# ---- vcpkg ----------------------------------------------------------------

# Resolve vcpkg: VCPKG_ROOT env var → VS-bundled → $HOME/.vcpkg
$VcpkgRoot = if ($env:VCPKG_ROOT) {
    $env:VCPKG_ROOT
} elseif (Test-Path 'C:\Program Files\Microsoft Visual Studio\18\Community\VC\vcpkg') {
    'C:\Program Files\Microsoft Visual Studio\18\Community\VC\vcpkg'
} else {
    Join-Path $HOME '.vcpkg'
}
$Toolchain = Join-Path $VcpkgRoot 'scripts' 'buildsystems' 'vcpkg.cmake'
if (-not (Test-Path $Toolchain)) {
    Write-Error "vcpkg not found at $VcpkgRoot. Set VCPKG_ROOT or install vcpkg."
    exit 1
}

# ---- markdown (C++ via CMake) ---------------------------------------------

Write-Host "==> Building markdown ($BuildType)"
$MarkdownBuild = Join-Path $MarkdownSrc 'build' $BuildType
# x64-windows-static so vcpkg builds static cmark-gfm (the
# CMakeLists links libcmark-gfm_static / _extensions_static).
$VcpkgTriplet = "x64-windows-static"
$cmakeArgs = @('-S', $MarkdownSrc, '-B', $MarkdownBuild,
    "-DCMAKE_BUILD_TYPE=$BuildType",
    "-DCMAKE_TOOLCHAIN_FILE=$Toolchain",
    "-DVCPKG_TARGET_TRIPLET=$VcpkgTriplet")
cmake @cmakeArgs 2>&1 | Out-Null
cmake --build $MarkdownBuild --config $BuildType --parallel
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

$DllPath = Join-Path $MarkdownBuild $BuildType 'pivox_markdown.dll'
$PdbPath = Join-Path $MarkdownBuild $BuildType 'pivox_markdown.pdb'
Copy-Item $DllPath -Destination $StageDir -Force
if (Test-Path $PdbPath) { Copy-Item $PdbPath -Destination $StageDir -Force }

# ---- highlight (Rust via Cargo) -------------------------------------------

Write-Host "==> Building highlight (cargo $CargoProfile)"

# Ensure MSVC link.exe is found before Git's /usr/bin/link.
# Cargo on Windows needs the MSVC linker; Git Bash shadows it.
$VsWhere = "${env:ProgramFiles(x86)}\Microsoft Visual Studio\Installer\vswhere.exe"
if (Test-Path $VsWhere) {
    $VsPath = & $VsWhere -latest -products * -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath
    if ($VsPath) {
        $MsvcBin = Get-ChildItem "$VsPath\VC\Tools\MSVC\*\bin\Hostx64\x64" -Directory | Select-Object -Last 1
        if ($MsvcBin) {
            $env:PATH = "$($MsvcBin.FullName);$env:PATH"
        }
    }
}

Push-Location $HighlightSrc
try {
    if ($CargoProfile -eq 'release') {
        cargo build --release
    } else {
        cargo build
    }
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
} finally {
    Pop-Location
}

$HighlightTarget = Join-Path $HighlightSrc 'target' $CargoProfileDir
$HlDll = Join-Path $HighlightTarget 'pivox_highlight.dll'
$HlPdb = Join-Path $HighlightTarget 'pivox_highlight.pdb'
Copy-Item $HlDll -Destination $StageDir -Force
if (Test-Path $HlPdb) { Copy-Item $HlPdb -Destination $StageDir -Force }

# ---- done -----------------------------------------------------------------

Write-Host "==> Done. Artifacts in ${StageDir}:"
Get-ChildItem $StageDir | Format-Table Name, Length -AutoSize
