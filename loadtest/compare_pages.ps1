param(
    [string]$Pdf = 'C:\Users\Theron Thayer\Downloads\dotnet-fundamentals.pdf',
    [int]$ArticleID = 18,
    [int]$ChunkPages = 200
)

$ErrorActionPreference = 'Continue'
$bin = 'D:\code\tools\poppler\poppler-24.08.0\Library\bin'
$tmp = Join-Path $env:TEMP 'opencode\compare'
New-Item -ItemType Directory -Force -Path $tmp | Out-Null
$md5 = [System.Security.Cryptography.MD5]::Create()

function Get-TextHash([string]$s) {
    -join ($md5.ComputeHash([System.Text.Encoding]::UTF8.GetBytes($s)) | ForEach-Object { $_.ToString('x2') })
}

# Cùng quy tắc với cleanPageText trong pdf.go: bỏ dòng trống, trim từng dòng.
function Get-CleanPage([string]$raw) {
    $lines = $raw.Split("`n") | ForEach-Object { $_.Trim() } | Where-Object { $_ -ne '' }
    (($lines -join "`n")).Trim()
}

$pdfHash = @{}
$total = [int](& "$bin\pdfinfo.exe" $Pdf | Select-String '^Pages:' | ForEach-Object { ($_.Line -split '\s+')[1] })
for ($from = 1; $from -le $total; $from += $ChunkPages) {
    $to = [Math]::Min($from + $ChunkPages - 1, $total)
    $out = Join-Path $tmp "range_$from.txt"
    & "$bin\pdftotext.exe" -enc UTF-8 -f $from -l $to $Pdf $out 2>$null
    $parts = [System.IO.File]::ReadAllText($out, [System.Text.Encoding]::UTF8).Split([char]12)
    for ($k = 0; $k -lt ($to - $from + 1); $k++) {
        $pdfHash[$from + $k] = Get-TextHash (Get-CleanPage $parts[$k])
    }
}

$rows = docker exec -i theron_postgres psql -U theron_user -d manager_db -At -c "select seq, md5(text) from article_pages where article_id=$ArticleID order by seq"
$dbMd5 = @{}
foreach ($r in $rows) { $p = $r.Split('|'); $dbMd5[[int]$p[0]] = $p[1] }

$mismatch = @()
for ($n = 1; $n -le $total; $n++) {
    if ($dbMd5[$n] -ne $pdfHash[$n]) { $mismatch += $n }
}
Write-Output "pdf pages: $total, db rows: $($dbMd5.Count), mismatched pages: $($mismatch.Count)"
$mismatch | Select-Object -First 10 | ForEach-Object { Write-Output "  page $_" }
