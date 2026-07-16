# Starts the Filler Arena API and worker in separate windows.
# Run from anywhere; paths are resolved relative to this script.
$arena = Split-Path -Parent $PSScriptRoot

docker compose -f (Join-Path $arena 'docker-compose.yml') up -d

# Logins granted admin (Admin tab: requeue matches, deactivate/delete bots).
$env:ARENA_ADMIN_LOGINS = 'mohamedahmed0'

Start-Process -WorkingDirectory $arena -FilePath (Join-Path $arena 'bin\api.exe')
Start-Process -WorkingDirectory $arena -FilePath (Join-Path $arena 'bin\worker.exe')

Write-Host "Filler Arena running: http://localhost:8080"
