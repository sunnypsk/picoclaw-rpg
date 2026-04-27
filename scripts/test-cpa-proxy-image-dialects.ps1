param(
    [string]$Prompt = 'A simple smoke-test image: a small red cube centered on a plain white background, clean composition, low detail.',
    [int]$Repeat = 1,
    [string]$ResponsesTextModel = 'gpt-5.5',
    [string]$CaseFilter = 'all',
    [int]$CurlMaxTimeSeconds = 360,
    [string]$PicoclawHome = '',
    [string]$EnvFile = '',
    [string]$OutputDir = ''
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path

function Resolve-PicoclawHome {
    param([string]$ConfiguredPath)

    if (-not [string]::IsNullOrWhiteSpace($ConfiguredPath)) {
        return (Resolve-Path -LiteralPath $ConfiguredPath).Path
    }

    $defaultPath = Join-Path $repoRoot 'docker\data'
    if (Test-Path -LiteralPath $defaultPath -PathType Container) {
        return (Resolve-Path -LiteralPath $defaultPath).Path
    }

    $homePath = Join-Path $HOME '.picoclaw'
    if (Test-Path -LiteralPath $homePath -PathType Container) {
        return (Resolve-Path -LiteralPath $homePath).Path
    }

    throw "Picoclaw home not found. Pass -PicoclawHome or -EnvFile explicitly."
}

function Resolve-EnvFile {
    param(
        [string]$ConfiguredPath,
        [string]$HomePath
    )

    if (-not [string]::IsNullOrWhiteSpace($ConfiguredPath)) {
        return (Resolve-Path -LiteralPath $ConfiguredPath).Path
    }

    $candidate = Join-Path $HomePath '.env'
    if (Test-Path -LiteralPath $candidate -PathType Leaf) {
        return (Resolve-Path -LiteralPath $candidate).Path
    }

    throw "Picoclaw env file not found: $candidate"
}

function Read-DotEnv {
    param([string]$Path)

    $values = @{}
    foreach ($line in Get-Content -LiteralPath $Path) {
        $trimmed = $line.Trim()
        if ([string]::IsNullOrWhiteSpace($trimmed) -or $trimmed.StartsWith('#')) {
            continue
        }
        $separator = $trimmed.IndexOf('=')
        if ($separator -lt 1) {
            continue
        }

        $key = $trimmed.Substring(0, $separator).Trim()
        $value = $trimmed.Substring($separator + 1).Trim()
        if (($value.StartsWith('"') -and $value.EndsWith('"')) -or ($value.StartsWith("'") -and $value.EndsWith("'"))) {
            $value = $value.Substring(1, $value.Length - 2)
        }
        $values[$key] = $value
    }
    return $values
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
    $targetDir = Join-Path $baseDir ("proxy-dialects-" + $timestamp)
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

function Test-CaseEnabled {
    param([string]$Name)

    if ($CaseFilter -eq 'all') {
        return $true
    }
    return $CaseFilter -like "*$Name*"
}

function Get-Base64ImageKind {
    param([string]$Value)

    if ([string]::IsNullOrWhiteSpace($Value) -or $Value.Length -lt 500) {
        return $null
    }

    $candidate = $Value
    if ($candidate.StartsWith('data:image/')) {
        $comma = $candidate.IndexOf(',')
        if ($comma -ge 0) {
            $candidate = $candidate.Substring($comma + 1)
        }
    }
    $candidate = ($candidate -replace '\s+', '')
    $sample = $candidate.Substring(0, [Math]::Min(4096, $candidate.Length))
    while (($sample.Length % 4) -ne 0) {
        $sample += '='
    }

    try {
        $bytes = [Convert]::FromBase64String($sample)
    } catch {
        return $null
    }

    if ($bytes.Length -ge 8 -and $bytes[0] -eq 0x89 -and $bytes[1] -eq 0x50 -and $bytes[2] -eq 0x4E -and $bytes[3] -eq 0x47) {
        return @{ Extension = 'png'; Base64 = $candidate }
    }
    if ($bytes.Length -ge 3 -and $bytes[0] -eq 0xFF -and $bytes[1] -eq 0xD8 -and $bytes[2] -eq 0xFF) {
        return @{ Extension = 'jpg'; Base64 = $candidate }
    }
    if ($bytes.Length -ge 12 -and $bytes[0] -eq 0x52 -and $bytes[1] -eq 0x49 -and $bytes[2] -eq 0x46 -and $bytes[3] -eq 0x46 -and $bytes[8] -eq 0x57 -and $bytes[9] -eq 0x45 -and $bytes[10] -eq 0x42 -and $bytes[11] -eq 0x50) {
        return @{ Extension = 'webp'; Base64 = $candidate }
    }

    return $null
}

function Search-JsonForImages {
    param(
        [object]$Node,
        [string]$Path = '$',
        [System.Collections.ArrayList]$Base64Candidates,
        [System.Collections.ArrayList]$UrlCandidates,
        [ref]$ImageGenerationCalls
    )

    if ($null -eq $Node) {
        return
    }

    if ($Node -is [System.Management.Automation.PSCustomObject]) {
        $properties = @($Node.PSObject.Properties)
        $typeProperty = $Node.PSObject.Properties['type']
        if ($null -ne $typeProperty -and $typeProperty.Value -eq 'image_generation_call') {
            $ImageGenerationCalls.Value++
        }

        foreach ($property in $properties) {
            $name = $property.Name
            $value = $property.Value
            $childPath = "$Path.$name"

            if ($value -is [string]) {
                $found = Get-Base64ImageKind -Value $value
                if ($null -ne $found) {
                    [void]$Base64Candidates.Add([pscustomobject]@{
                        Path      = $childPath
                        Extension = $found.Extension
                        Base64    = $found.Base64
                    })
                }

                $lowerName = $name.ToLowerInvariant()
                if (($lowerName -eq 'url' -or $lowerName -eq 'image_url') -and ($value.StartsWith('http://') -or $value.StartsWith('https://') -or $value.StartsWith('data:image/'))) {
                    [void]$UrlCandidates.Add([pscustomobject]@{
                        Path    = $childPath
                        Preview = $value.Substring(0, [Math]::Min(120, $value.Length))
                    })
                }
            }

            Search-JsonForImages -Node $value -Path $childPath -Base64Candidates $Base64Candidates -UrlCandidates $UrlCandidates -ImageGenerationCalls $ImageGenerationCalls
        }
        return
    }

    if ($Node -is [System.Collections.IEnumerable] -and -not ($Node -is [string])) {
        $index = 0
        foreach ($item in $Node) {
            Search-JsonForImages -Node $item -Path ("{0}[{1}]" -f $Path, $index) -Base64Candidates $Base64Candidates -UrlCandidates $UrlCandidates -ImageGenerationCalls $ImageGenerationCalls
            $index++
        }
    }
}

function Summarize-Response {
    param(
        [string]$ResponsePath,
        [string]$ImagePrefix
    )

    if (-not (Test-Path -LiteralPath $ResponsePath -PathType Leaf)) {
        Write-Host 'response_missing=true'
        return
    }

    $bytes = [System.IO.File]::ReadAllBytes($ResponsePath)
    Write-Host "response_bytes=$($bytes.Length)"
    $raw = [System.Text.Encoding]::UTF8.GetString($bytes)

    try {
        $data = $raw | ConvertFrom-Json
    } catch {
        $preview = $raw.Substring(0, [Math]::Min(220, $raw.Length)).Replace("`n", ' ')
        Write-Host "json_parse=failed:$($_.Exception.GetType().Name)"
        Write-Host "response_preview=$preview"
        return
    }

    if ($data -is [System.Management.Automation.PSCustomObject]) {
        $keys = @($data.PSObject.Properties | Select-Object -ExpandProperty Name | Sort-Object | Select-Object -First 30)
        Write-Host ("top_level_keys=" + ($keys -join ','))
        $errorProperty = $data.PSObject.Properties['error']
        if ($null -ne $errorProperty) {
            $err = $errorProperty.Value
            if ($err -is [System.Management.Automation.PSCustomObject]) {
                $messageProperty = $err.PSObject.Properties['message']
                $codeProperty = $err.PSObject.Properties['code']
                if ($null -ne $messageProperty) {
                    $message = [string]$messageProperty.Value
                } elseif ($null -ne $codeProperty) {
                    $message = [string]$codeProperty.Value
                } else {
                    $message = ($err | ConvertTo-Json -Compress)
                }
            } else {
                $message = [string]$err
            }
            $message = $message.Substring(0, [Math]::Min(260, $message.Length)).Replace("`n", ' ')
            Write-Host "error=$message"
        }
    }

    $base64Candidates = New-Object System.Collections.ArrayList
    $urlCandidates = New-Object System.Collections.ArrayList
    $imageGenerationCalls = 0
    Search-JsonForImages -Node $data -Base64Candidates $base64Candidates -UrlCandidates $urlCandidates -ImageGenerationCalls ([ref]$imageGenerationCalls)

    Write-Host "image_generation_calls=$imageGenerationCalls"
    Write-Host "image_base64_candidates=$($base64Candidates.Count)"
    Write-Host "image_url_candidates=$($urlCandidates.Count)"

    if ($urlCandidates.Count -gt 0) {
        Write-Host "first_image_url_path=$($urlCandidates[0].Path)"
        Write-Host "first_image_url_preview=$($urlCandidates[0].Preview)"
    }

    if ($base64Candidates.Count -gt 0) {
        $candidate = $base64Candidates[0]
        $imagePath = "$ImagePrefix.$($candidate.Extension)"
        $b64 = [string]$candidate.Base64
        while (($b64.Length % 4) -ne 0) {
            $b64 += '='
        }
        [System.IO.File]::WriteAllBytes($imagePath, [Convert]::FromBase64String($b64))
        Write-Host "image_saved=$imagePath"
    }
}

function Invoke-Case {
    param(
        [string]$Name,
        [string]$Endpoint,
        [string]$PayloadPath,
        [string]$BaseUrl,
        [string]$ApiKey,
        [string]$DestinationDir
    )

    $responsePath = Join-Path $DestinationDir "$Name.response.json"
    $metaPath = Join-Path $DestinationDir "$Name.meta.txt"

    Write-Host "=== $Name ==="
    Write-Host "endpoint=$Endpoint"
    Get-Date

    $curl = Get-Command curl.exe -ErrorAction SilentlyContinue
    if ($null -eq $curl) {
        throw 'curl.exe is required for stable HTTP timing output on Windows.'
    }

    $arguments = @(
        '-sS',
        '--connect-timeout', '20',
        '--max-time', [string]$CurlMaxTimeSeconds,
        '-o', $responsePath,
        '-w', "HTTP=%{http_code}`nTIME=%{time_total}`n",
        '-H', "Authorization: Bearer $ApiKey",
        '-H', 'Content-Type: application/json',
        '-H', 'User-Agent: picoclaw/1.0',
        ($BaseUrl.TrimEnd('/') + $Endpoint),
        '--data-binary', "@$PayloadPath"
    )

    $output = & $curl.Source @arguments 2>&1
    $exitCode = $LASTEXITCODE
    $outputText = ($output | Out-String).TrimEnd()
    Write-Host $outputText
    Write-Utf8NoBomFile -Path $metaPath -Content ($outputText + "`ncurl_exit=$exitCode`n")
    Write-Host "curl_exit=$exitCode"
    Summarize-Response -ResponsePath $responsePath -ImagePrefix (Join-Path $DestinationDir "$Name.image")
    Write-Host "payload_saved=$PayloadPath"
    Write-Host "response_saved=$responsePath"
    Get-Date
    Write-Host ''
}

$resolvedHome = Resolve-PicoclawHome -ConfiguredPath $PicoclawHome
$resolvedEnvFile = Resolve-EnvFile -ConfiguredPath $EnvFile -HomePath $resolvedHome
$envValues = Read-DotEnv -Path $resolvedEnvFile

foreach ($required in @('CPA_API_BASE', 'CPA_API_KEY', 'CPA_IMAGE_MODEL')) {
    if (-not $envValues.ContainsKey($required) -or [string]::IsNullOrWhiteSpace($envValues[$required])) {
        throw "$required is required in $resolvedEnvFile"
    }
}

$apiBase = $envValues['CPA_API_BASE']
$apiKey = $envValues['CPA_API_KEY']
$imageModel = $envValues['CPA_IMAGE_MODEL']
$resolvedOutputDir = New-OutputDirectory -ConfiguredPath $OutputDir -HomePath $resolvedHome

Write-Host "out_dir=$resolvedOutputDir"
Write-Host "image_model=$imageModel"
Write-Host "responses_text_model=$ResponsesTextModel"
Write-Host "repeat=$Repeat"
Write-Host "case_filter=$CaseFilter"
Write-Host ''

$payloads = @{
    images_generations_official = @{
        model      = $imageModel
        prompt     = $Prompt
        quality    = 'low'
        background = 'auto'
        size       = '1024x1024'
    }
    responses_official_tool_text_model = @{
        model       = $ResponsesTextModel
        input       = "Generate an image: $Prompt"
        tools       = @(@{ type = 'image_generation'; size = '1024x1024'; quality = 'low'; background = 'auto' })
        tool_choice = @{ type = 'image_generation' }
    }
    responses_direct_image_model = @{
        model      = $imageModel
        input      = "Generate an image: $Prompt"
        size       = '1024x1024'
        quality    = 'low'
        background = 'auto'
    }
    responses_tool_image_model = @{
        model       = $imageModel
        input       = "Generate an image: $Prompt"
        tools       = @(@{ type = 'image_generation'; size = '1024x1024'; quality = 'low'; background = 'auto' })
        tool_choice = @{ type = 'image_generation' }
    }
    chat_direct_image_model_minimal = @{
        model    = $imageModel
        messages = @(@{ role = 'user'; content = "Generate an image: $Prompt" })
    }
    chat_direct_image_model_modalities = @{
        model      = $imageModel
        messages   = @(@{ role = 'user'; content = "Generate an image: $Prompt" })
        modalities = @('image')
        size       = '1024x1024'
        quality    = 'low'
        background = 'auto'
    }
    chat_tool_image_model = @{
        model       = $imageModel
        messages    = @(@{ role = 'user'; content = "Generate an image: $Prompt" })
        tools       = @(@{ type = 'image_generation'; size = '1024x1024'; quality = 'low'; background = 'auto' })
        tool_choice = @{ type = 'image_generation' }
    }
}

$payloadPaths = @{}
foreach ($name in $payloads.Keys) {
    $path = Join-Path $resolvedOutputDir "$name.json"
    Write-Utf8NoBomFile -Path $path -Content ($payloads[$name] | ConvertTo-Json -Depth 12)
    $payloadPaths[$name] = $path
}

for ($i = 1; $i -le $Repeat; $i++) {
    Write-Host "######## repeat $i/$Repeat ########"

    if (Test-CaseEnabled -Name 'images') {
        Invoke-Case -Name "r${i}_images_generations_official" -Endpoint '/images/generations' -PayloadPath $payloadPaths.images_generations_official -BaseUrl $apiBase -ApiKey $apiKey -DestinationDir $resolvedOutputDir
    }
    if (Test-CaseEnabled -Name 'responses_official') {
        Invoke-Case -Name "r${i}_responses_official_tool_text_model" -Endpoint '/responses' -PayloadPath $payloadPaths.responses_official_tool_text_model -BaseUrl $apiBase -ApiKey $apiKey -DestinationDir $resolvedOutputDir
    }
    if (Test-CaseEnabled -Name 'responses_direct') {
        Invoke-Case -Name "r${i}_responses_direct_image_model" -Endpoint '/responses' -PayloadPath $payloadPaths.responses_direct_image_model -BaseUrl $apiBase -ApiKey $apiKey -DestinationDir $resolvedOutputDir
    }
    if (Test-CaseEnabled -Name 'responses_tool') {
        Invoke-Case -Name "r${i}_responses_tool_image_model" -Endpoint '/responses' -PayloadPath $payloadPaths.responses_tool_image_model -BaseUrl $apiBase -ApiKey $apiKey -DestinationDir $resolvedOutputDir
    }
    if (Test-CaseEnabled -Name 'chat_minimal') {
        Invoke-Case -Name "r${i}_chat_direct_image_model_minimal" -Endpoint '/chat/completions' -PayloadPath $payloadPaths.chat_direct_image_model_minimal -BaseUrl $apiBase -ApiKey $apiKey -DestinationDir $resolvedOutputDir
    }
    if (Test-CaseEnabled -Name 'chat_modalities') {
        Invoke-Case -Name "r${i}_chat_direct_image_model_modalities" -Endpoint '/chat/completions' -PayloadPath $payloadPaths.chat_direct_image_model_modalities -BaseUrl $apiBase -ApiKey $apiKey -DestinationDir $resolvedOutputDir
    }
    if (Test-CaseEnabled -Name 'chat_tool') {
        Invoke-Case -Name "r${i}_chat_tool_image_model" -Endpoint '/chat/completions' -PayloadPath $payloadPaths.chat_tool_image_model -BaseUrl $apiBase -ApiKey $apiKey -DestinationDir $resolvedOutputDir
    }
}

Write-Host "Done. Full responses are in: $resolvedOutputDir"
