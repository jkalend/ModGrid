param(
    [Parameter(Mandatory = $true)][string]$Root,
    [switch]$AcceptEula
)
$ErrorActionPreference = 'Stop'
if (-not $AcceptEula) { throw 'Explicit owner EULA acceptance is required. Read https://www.minecraft.net/en-us/eula before passing -AcceptEula.' }
$pinsPath = Join-Path $PSScriptRoot '../tests/runtime/pins.json'
$runtimePins = Get-Content -Raw -LiteralPath $pinsPath | ConvertFrom-Json
$platformPin = if ($IsWindows) { $runtimePins.windows } else { $runtimePins.linux }
$runtimeRoot = [System.IO.Path]::GetFullPath($Root)
if (Test-Path -LiteralPath $runtimeRoot) { throw 'Runtime preparation requires an absent directory.' }
New-Item -ItemType Directory -Path $runtimeRoot -Force | Out-Null
$jdkArchivePath = Join-Path $runtimeRoot $(if ($IsWindows) { 'jdk.zip' } else { 'jdk.tar.gz' })
Invoke-WebRequest -Uri $platformPin.url -OutFile $jdkArchivePath
if ((Get-FileHash -Algorithm SHA256 -LiteralPath $jdkArchivePath).Hash.ToLowerInvariant() -ne $platformPin.sha256) { throw 'Java distribution checksum mismatch.' }
if ($IsWindows) { Expand-Archive -LiteralPath $jdkArchivePath -DestinationPath $runtimeRoot }
else { & tar -xzf $jdkArchivePath -C $runtimeRoot; if ($LASTEXITCODE -ne 0) { throw 'Java distribution extraction failed.' } }
$javaPath = Join-Path $runtimeRoot ($platformPin.root + $(if ($IsWindows) { '/bin/java.exe' } else { '/bin/java' }))
$serverRoot = Join-Path $runtimeRoot 'server'
New-Item -ItemType Directory -Path $serverRoot | Out-Null
$launcherPath = Join-Path $serverRoot 'fabric-server-launch.jar'
Invoke-WebRequest -Uri $runtimePins.launcher_url -OutFile $launcherPath
if ((Get-FileHash -Algorithm SHA512 -LiteralPath $launcherPath).Hash.ToLowerInvariant() -ne $runtimePins.launcher_sha512) { throw 'Fabric launcher checksum mismatch.' }
$utf8Encoding = [System.Text.UTF8Encoding]::new($false)
[System.IO.File]::WriteAllText((Join-Path $serverRoot 'server.properties'), "server-ip=127.0.0.1`nserver-port=25577`nenable-rcon=false`nonline-mode=true`nwhite-list=true`nenforce-whitelist=true`nmax-players=1`nview-distance=2`nsimulation-distance=2`nlevel-seed=246813579`n", $utf8Encoding)
[System.IO.File]::WriteAllText((Join-Path $serverRoot 'eula.txt'), "eula=false`n", $utf8Encoding)
Push-Location -LiteralPath $serverRoot
try {
    & $javaPath -Xms256M -Xmx1G -jar fabric-server-launch.jar nogui *> bootstrap.log
    if ($LASTEXITCODE -ne 0) { throw 'Pinned Fabric bootstrap failed; inspect the private bootstrap.log.' }
} finally { Pop-Location }
# This script records only the acceptance explicitly supplied by the owner.
[System.IO.File]::WriteAllText((Join-Path $serverRoot 'eula.txt'), "# Owner explicitly accepted the Minecraft EULA for disposable fixtures.`neula=true`n", $utf8Encoding)
$result = [ordered]@{runtime=$serverRoot;java=$javaPath;build=$platformPin.build;distribution_sha256=('sha256:' + $platformPin.sha256)}
if ($env:GITHUB_ENV) {
    @("MODGRID_TEST_RUNTIME=$serverRoot", "MODGRID_TEST_JAVA=$javaPath", "MODGRID_TEST_JAVA_BUILD=$($platformPin.build)", "MODGRID_TEST_JAVA_DISTRIBUTION_SHA256=sha256:$($platformPin.sha256)") | Out-File -LiteralPath $env:GITHUB_ENV -Append -Encoding utf8
}
$result | ConvertTo-Json
