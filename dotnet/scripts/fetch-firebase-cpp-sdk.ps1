# fetch-firebase-cpp-sdk.ps1 — Download and extract the Firebase C++ SDK
# for the Pivox.Firebase.Native C++/WinRT component.
#
# Run once per developer machine (or after a version bump).
# The extracted SDK is gitignored — this script is the reproducible source.
#
# Usage:
#   pwsh -File dotnet/scripts/fetch-firebase-cpp-sdk.ps1
#
# Requires: PowerShell 7+ (pwsh), internet access.

$ErrorActionPreference = 'Stop'

$Version = '13.7.0'
$Url = "https://dl.google.com/firebase/sdk/cpp/firebase_cpp_sdk_${Version}.zip"
$DestDir = Join-Path $PSScriptRoot '..' 'Pivox.Firebase.Native' 'firebase_cpp_sdk'
$ZipPath = Join-Path $PSScriptRoot '..' '.cache' "firebase_cpp_sdk_${Version}.zip"

# Skip if already extracted.
if (Test-Path (Join-Path $DestDir 'include' 'firebase' 'app.h')) {
    Write-Host "Firebase C++ SDK $Version already present at $DestDir"
    exit 0
}

# Download.
$CacheDir = Split-Path $ZipPath
if (-not (Test-Path $CacheDir)) { New-Item -ItemType Directory -Path $CacheDir -Force | Out-Null }

if (-not (Test-Path $ZipPath)) {
    Write-Host "Downloading Firebase C++ SDK $Version..."
    Invoke-WebRequest -Uri $Url -OutFile $ZipPath -UseBasicParsing
    Write-Host "Downloaded to $ZipPath"
} else {
    Write-Host "Using cached download at $ZipPath"
}

# Extract.
Write-Host "Extracting to $DestDir..."
if (Test-Path $DestDir) { Remove-Item -Recurse -Force $DestDir }

$TempDir = Join-Path $CacheDir "firebase_extract_$$"
Expand-Archive -Path $ZipPath -DestinationPath $TempDir -Force

# The zip extracts to firebase_cpp_sdk_${Version}/ — move contents.
$Inner = Get-ChildItem -Path $TempDir -Directory | Select-Object -First 1
if ($Inner) {
    Move-Item -Path $Inner.FullName -Destination $DestDir
} else {
    Move-Item -Path $TempDir -Destination $DestDir
}

# Clean up temp.
if (Test-Path $TempDir) { Remove-Item -Recurse -Force $TempDir }

Write-Host "Firebase C++ SDK $Version ready at $DestDir"

# Verify key files exist.
$AppHeader = Join-Path $DestDir 'include' 'firebase' 'app.h'
$AuthHeader = Join-Path $DestDir 'include' 'firebase' 'auth.h'
if (-not (Test-Path $AppHeader) -or -not (Test-Path $AuthHeader)) {
    Write-Error "Extraction incomplete — missing firebase/app.h or firebase/auth.h"
    exit 1
}

Write-Host "Verification passed."
