param(
    [string]$Prompt = 'A minimal test image: a small red cube centered on a white background.',
    [string]$PicoclawHome = '',
    [string]$OutputDir = '',
    [string]$OutputPrefix = 'smoke'
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$helperPath = Join-Path $repoRoot 'workspace\skills\generate-image\scripts\generate_image.py'

function Get-PythonInvocation {
    if (Get-Command python -ErrorAction SilentlyContinue) {
        return @{
            Command = 'python'
            Args    = @()
        }
    }
    if (Get-Command py -ErrorAction SilentlyContinue) {
        return @{
            Command = 'py'
            Args    = @('-3')
        }
    }
    throw 'Python 3 is required. Install python or use the py launcher.'
}

function Resolve-PicoclawHome {
    param([string]$ConfiguredPath)

    if (-not [string]::IsNullOrWhiteSpace($ConfiguredPath)) {
        return (Resolve-Path -LiteralPath $ConfiguredPath).Path
    }

    $defaultPath = Join-Path $repoRoot 'docker\data'
    if (-not (Test-Path -LiteralPath $defaultPath -PathType Container)) {
        throw "Default Picoclaw home not found: $defaultPath"
    }
    return (Resolve-Path -LiteralPath $defaultPath).Path
}

function New-OutputDirectory {
    param(
        [string]$ConfiguredPath,
        [string]$HomePath
    )

    if (-not [string]::IsNullOrWhiteSpace($ConfiguredPath)) {
        New-Item -ItemType Directory -Force -Path $ConfiguredPath | Out-Null
        return (Resolve-Path -LiteralPath $ConfiguredPath).Path
    }

    $baseDir = Join-Path $HomePath 'image-smoke-tests'
    $timestamp = Get-Date -Format 'yyyyMMdd-HHmmss'
    $targetDir = Join-Path $baseDir $timestamp
    New-Item -ItemType Directory -Force -Path $targetDir | Out-Null
    return (Resolve-Path -LiteralPath $targetDir).Path
}

function Write-Utf8NoBomFile {
    param(
        [string]$Path,
        [string]$Content
    )

    $utf8NoBom = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllText($Path, $Content, $utf8NoBom)
}

function Invoke-GenerateImageHelper {
    param(
        [hashtable]$Python,
        [string]$HomePath,
        [string]$PayloadPath,
        [string]$DestinationDir,
        [string]$Prefix
    )

    $stdoutPath = Join-Path $DestinationDir 'helper.stdout.json'
    $stderrPath = Join-Path $DestinationDir 'helper.stderr.txt'
    $arguments = @()
    $arguments += $Python.Args
    $arguments += @(
        $helperPath,
        '--payload-file', $PayloadPath,
        '--output-dir', $DestinationDir,
        '--output-prefix', $Prefix
    )

    $previousHome = $env:PICOCLAW_HOME
    $env:PICOCLAW_HOME = $HomePath
    try {
        $process = Start-Process `
            -FilePath $Python.Command `
            -ArgumentList $arguments `
            -NoNewWindow `
            -Wait `
            -PassThru `
            -RedirectStandardOutput $stdoutPath `
            -RedirectStandardError $stderrPath
    } finally {
        if ($null -eq $previousHome) {
            Remove-Item Env:PICOCLAW_HOME -ErrorAction SilentlyContinue
        } else {
            $env:PICOCLAW_HOME = $previousHome
        }
    }

    return [pscustomobject]@{
        ExitCode = $process.ExitCode
        Stdout   = if (Test-Path -LiteralPath $stdoutPath) { Get-Content -LiteralPath $stdoutPath -Raw } else { '' }
        Stderr   = if (Test-Path -LiteralPath $stderrPath) { Get-Content -LiteralPath $stderrPath -Raw } else { '' }
        StdoutPath = $stdoutPath
        StderrPath = $stderrPath
    }
}

if (-not (Test-Path -LiteralPath $helperPath -PathType Leaf)) {
    throw "Generate-image helper not found: $helperPath"
}

$resolvedHome = Resolve-PicoclawHome -ConfiguredPath $PicoclawHome
$envFile = Join-Path $resolvedHome '.env'
if (-not (Test-Path -LiteralPath $envFile -PathType Leaf)) {
    throw "Picoclaw env file not found: $envFile"
}

$resolvedOutputDir = New-OutputDirectory -ConfiguredPath $OutputDir -HomePath $resolvedHome
$python = Get-PythonInvocation

$tempDir = Join-Path ([System.IO.Path]::GetTempPath()) ('picoclaw-image-smoke-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force -Path $tempDir | Out-Null
$payloadPath = Join-Path $tempDir 'payload.json'
$requestLogPath = Join-Path $resolvedOutputDir 'request.json'
$resultLogPath = Join-Path $resolvedOutputDir 'result.json'

try {
    $payload = @{
        prompt = $Prompt
    }
    $payloadJson = $payload | ConvertTo-Json -Depth 4
    Write-Utf8NoBomFile -Path $payloadPath -Content $payloadJson
    Write-Utf8NoBomFile -Path $requestLogPath -Content $payloadJson

    $result = Invoke-GenerateImageHelper `
        -Python $python `
        -HomePath $resolvedHome `
        -PayloadPath $payloadPath `
        -DestinationDir $resolvedOutputDir `
        -Prefix $OutputPrefix

    if ($result.ExitCode -ne 0) {
        $errorText = $result.Stderr.Trim()
        if ([string]::IsNullOrWhiteSpace($errorText)) {
            $errorText = $result.Stdout.Trim()
        }
        throw "Image smoke test failed with exit code $($result.ExitCode): $errorText"
    }

    if ([string]::IsNullOrWhiteSpace($result.Stdout)) {
        throw 'Image smoke test failed: helper returned empty stdout.'
    }

    Write-Utf8NoBomFile -Path $resultLogPath -Content $result.Stdout
    $parsed = $result.Stdout | ConvertFrom-Json
    $images = @($parsed.images)
    if ($images.Count -eq 0) {
        throw 'Image smoke test failed: helper returned no image paths.'
    }

    $writtenFiles = @()
    foreach ($imagePath in $images) {
        if (Test-Path -LiteralPath $imagePath -PathType Leaf) {
            $item = Get-Item -LiteralPath $imagePath
            $writtenFiles += [pscustomobject]@{
                Path      = $item.FullName
                SizeBytes = $item.Length
            }
        }
    }

    if ($writtenFiles.Count -eq 0) {
        throw 'Image smoke test failed: helper reported images, but no output files were found.'
    }

    Write-Host 'Image smoke test succeeded.' -ForegroundColor Green
    Write-Host "Model:      $($parsed.model)"
    Write-Host "Provider:   $($parsed.provider)"
    Write-Host "Home:       $resolvedHome"
    Write-Host "Output dir: $resolvedOutputDir"
    Write-Host "Request:    $requestLogPath"
    Write-Host "Result:     $resultLogPath"
    Write-Host 'Files:'
    foreach ($file in $writtenFiles) {
        Write-Host ("- {0} ({1} bytes)" -f $file.Path, $file.SizeBytes)
    }
} finally {
    Remove-Item -LiteralPath $tempDir -Recurse -Force -ErrorAction SilentlyContinue
}
