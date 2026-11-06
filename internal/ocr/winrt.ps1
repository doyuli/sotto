# Windows.Media.Ocr over a stdin/stdout line-JSON protocol, for gobob's
# resident OCR subprocess (research.md §5.6). Windows PowerShell 5.1 only:
# the WinRT projection does not work in pwsh.
#
# Protocol: one JSON object per line.
#   in:  {"png":"<base64>","lang":"zh-Hans"}    lang optional
#   out: {"ok":true,"text":"...","lines":[{"text":"...","box":[x,y,w,h]}]}
#        {"ok":false,"error":"..."}
# Startup emits {"ready":true,"languages":[...]} once the engine is up,
# or {"ready":false,"error":"..."} when no OCR language pack is usable.

$ErrorActionPreference = 'Stop'
$OutputEncoding = New-Object System.Text.UTF8Encoding($false)
Add-Type -AssemblyName System.Runtime.WindowsRuntime

# Await the IAsyncOperation`1 WinRT returns, as a plain .NET task.
$script:asTask = ([System.WindowsRuntimeSystemExtensions].GetMethods() |
  Where-Object { $_.Name -eq 'AsTask' -and $_.GetParameters().Count -eq 1 -and
    $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncOperation`1' })[0]
function Await($op, $resultType) {
  $task = $script:asTask.MakeGenericMethod($resultType).Invoke($null, @($op))
  $task.Wait(-1) | Out-Null
  $task.Result
}

# Load the WinRT projections the pipeline needs.
$null = [Windows.Media.Ocr.OcrEngine, Windows.Foundation, ContentType=WindowsRuntime]
$null = [Windows.Graphics.Imaging.BitmapDecoder, Windows.Foundation, ContentType=WindowsRuntime]
$null = [Windows.Globalization.Language, Windows.Foundation, ContentType=WindowsRuntime]

$script:engines = @{}
$script:defaultEngine = $null

function New-Engine([string]$langTag) {
  if ($langTag) {
    $lang = New-Object Windows.Globalization.Language($langTag)
    return [Windows.Media.Ocr.OcrEngine]::TryCreateFromLanguage($lang)
  }
  return [Windows.Media.Ocr.OcrEngine]::TryCreateFromUserProfileLanguages()
}

function Get-Engine([string]$langTag) {
  if (-not $langTag) {
    if (-not $script:defaultEngine) {
      $script:defaultEngine = New-Engine ''
    }
    return $script:defaultEngine
  }
  if (-not $script:engines.ContainsKey($langTag)) {
    $script:engines[$langTag] = New-Engine $langTag
  }
  return $script:engines[$langTag]
}

function Send-Line([string]$line) {
  $bytes = [System.Text.Encoding]::UTF8.GetBytes($line + "`n")
  [Console]::OpenStandardOutput().Write($bytes, 0, $bytes.Length)
}

# Languages the installed packs cover, as BCP-47 tags.
$langs = @()
try {
  [Windows.Media.Ocr.OcrEngine]::AvailableRecognizerLanguages | ForEach-Object { $langs += $_.LanguageTag }
} catch {}

$default = New-Engine ''
if (-not $default) {
  Send-Line ('{"ready":false,"error":"no OCR language pack installed"}')
  exit 1
}
Send-Line (('{"ready":true,"languages":' + (ConvertTo-Json -InputObject @($langs) -Compress) + '}'))

$stdin = [Console]::OpenStandardInput()
while ($true) {
  # Read one UTF-8 line by hand: the console input encoding is not
  # reliable under redirection.
  $ms = New-Object System.IO.MemoryStream
  while ($true) {
    $b = $stdin.ReadByte()
    if ($b -lt 0) { return }           # stdin closed: exit
    if ($b -eq 10) { break }           # LF ends the line
    if ($b -ne 13) { $ms.WriteByte($b) }
  }
  if ($ms.Length -eq 0) { continue }
  $line = [System.Text.Encoding]::UTF8.GetString($ms.ToArray())
  try {
    $req = $line | ConvertFrom-Json
    $engine = Get-Engine $req.lang
    if (-not $engine) {
      Send-Line '{"ok":false,"error":"language pack not available"}'
      continue
    }
    $bytes = [Convert]::FromBase64String($req.png)
    if ($bytes.Length -eq 0) {
      Send-Line '{"ok":false,"error":"empty image"}'
      continue
    }
    $stream = New-Object System.IO.MemoryStream (,$bytes)
    $ras = [System.IO.WindowsRuntimeStreamExtensions]::AsRandomAccessStream($stream)
    $decoder = Await ([Windows.Graphics.Imaging.BitmapDecoder]::CreateAsync($ras)) ([Windows.Graphics.Imaging.BitmapDecoder])
    $bitmap = Await ($decoder.GetSoftwareBitmapAsync()) ([Windows.Graphics.Imaging.SoftwareBitmap])
    $result = Await ($engine.RecognizeAsync($bitmap)) ([Windows.Media.Ocr.OcrResult])

    # A Windows OCR engine recognizes its own language only: an English
    # engine reads no Chinese at all. When the user-profile engine comes
    # back empty (or nearly so), try every other installed engine and keep
    # the longest text — the matching pack is then found without asking.
    if (-not $req.lang -and $langs.Count -gt 1 -and (($result.Text -replace '\s', '').Length -lt 3)) {
      $best = $result
      foreach ($tag in $langs) {
        try {
          $alt = Get-Engine $tag
          if (-not $alt) { continue }
          $r2 = Await ($alt.RecognizeAsync($bitmap)) ([Windows.Media.Ocr.OcrResult])
          if (($r2.Text -replace '\s', '').Length -gt ($best.Text -replace '\s', '').Length) {
            $best = $r2
          }
        } catch {}
      }
      $result = $best
    }

    # The WinRT collections come through as __ComObject: piping them
    # enumerates the elements, while @(...) can wrap the whole collection
    # as a single item (which made every line carry the full text).
    # Line text comes from OcrLine.Text so it stays paired with its box;
    # joining words would place a space between every CJK glyph.
    $lines = @()
    foreach ($l in $result.Lines) {
      $words = @()
      foreach ($w in $l.Words) { $words += $w }
      if ($words.Count -eq 0) { continue }
      $first = $words[0].BoundingRect
      $x = [int]$first.X
      $y = [int]$first.Y
      $right = [int]($first.X + $first.Width)
      $bottom = [int]($first.Y + $first.Height)
      foreach ($w in $words) {
        $br = $w.BoundingRect
        if ([int]$br.X -lt $x) { $x = [int]$br.X }
        if ([int]$br.Y -lt $y) { $y = [int]$br.Y }
        if ([int]($br.X + $br.Width) -gt $right) { $right = [int]($br.X + $br.Width) }
        if ([int]($br.Y + $br.Height) -gt $bottom) { $bottom = [int]($br.Y + $br.Height) }
      }
      $text = $l.Text
      if (-not $text) { $text = ($words | ForEach-Object { $_.Text }) -join '' }
      # Parentheses around the subtractions: comma binds tighter than
      # minus, so $right - $x, $bottom - $y would subtract an array.
      $lines += @{ text = $text; box = @($x, $y, ($right - $x), ($bottom - $y)) }
    }
    $out = @{ ok = $true; text = $result.Text; lines = @($lines) }
    Send-Line (ConvertTo-Json -InputObject $out -Compress -Depth 4)
  } catch {
    $msg = ($_.Exception.Message -replace '"', "'")
    $at = ''
    if ($_.InvocationInfo -and $_.InvocationInfo.ScriptLineNumber) {
      $at = ' (line ' + $_.InvocationInfo.ScriptLineNumber + ')'
    }
    Send-Line ('{"ok":false,"error":"' + $msg + $at + '"}')
  }
}
