# Run FlaUI UI tests against Firebase Auth Emulator.
# Usage: pwsh run-tests.ps1

$ErrorActionPreference = "Stop"

Write-Host "Starting Firebase Auth Emulator..."
$emuProcess = Start-Process -FilePath "firebase" `
    -ArgumentList "emulators:start --only auth --project pivox-cloud" `
    -PassThru -NoNewWindow

# Wait for emulator to be ready.
$maxWait = 30
for ($i = 0; $i -lt $maxWait; $i++) {
    try {
        $response = Invoke-WebRequest -Uri "http://127.0.0.1:9099/" -TimeoutSec 1 -ErrorAction SilentlyContinue
        if ($response.StatusCode -eq 200) {
            Write-Host "Auth Emulator is ready."
            break
        }
    } catch { }
    Start-Sleep -Seconds 1
    if ($i -eq ($maxWait - 1)) {
        Write-Host "ERROR: Auth Emulator did not start within $maxWait seconds."
        Stop-Process -Id $emuProcess.Id -Force -ErrorAction SilentlyContinue
        exit 1
    }
}

try {
    Write-Host "Running FlaUI UI tests..."
    dotnet test $PSScriptRoot --logger "console;verbosity=normal"
    $testExitCode = $LASTEXITCODE
} finally {
    Write-Host "Stopping Firebase Auth Emulator..."
    Stop-Process -Id $emuProcess.Id -Force -ErrorAction SilentlyContinue
    # Also kill any lingering Java processes from the emulator.
    Get-Process -Name "java" -ErrorAction SilentlyContinue |
        Where-Object { $_.CommandLine -match "emulator" } |
        Stop-Process -Force -ErrorAction SilentlyContinue
}

exit $testExitCode
