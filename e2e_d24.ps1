$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [Text.Encoding]::UTF8
$b = 'http://127.0.0.1:9001/api/v1'

# Invoke-RestMethod của PowerShell 5.1 mã hoá body theo ANSI nếu chỉ đưa string,
# nên câu hỏi tiếng Việt có dấu sẽ thành rác khi gửi → phải tự đẩy byte UTF-8.
function Post-Json($uri, $obj, $headers, $secs) {
  $bytes = [Text.Encoding]::UTF8.GetBytes(($obj | ConvertTo-Json -Depth 6))
  return Invoke-RestMethod -Uri $uri -Method Post -Headers $headers -ContentType 'application/json; charset=utf-8' `
    -Body $bytes -TimeoutSec $secs
}
function Show($label, $obj) {
  Write-Output "=== $label ==="
  $obj | ConvertTo-Json -Depth 8
}

# 1) login user đã có tài liệu index (category 16: quy_dinh_noi_bo + quy_dinh_bo_sung)
$tok = (Post-Json "$b/auth/login" @{ username = $env:D24_USER; password = 'secret123' } @{} 30).data.token
$h = @{ Authorization = "Bearer $tok" }
Write-Output "token length = $($tok.Length)"

# 2) tạo phiên (title rỗng — sẽ tự điền từ câu hỏi đầu)
$conv = (Post-Json "$b/conversations" @{ title = ''; provider = 'ollama'; category_ids = @(16) } $h 30).data
Show 'create conversation' $conv
$id = $conv.id

# 3) lượt 1 — client KHÔNG gửi history, chỉ conversation_id
$ask1 = 'Quy định nội bộ của công ty nói về vấn đề gì?'
$r1 = (Post-Json "$b/chat" @{ message = $ask1; conversation_id = $id; limit = 3 } $h 600).data
Write-Output '=== turn 1 answer (300 chars đầu) ==='
Write-Output $r1.answer.Substring(0, [Math]::Min(300, $r1.answer.Length))
Write-Output "cites = $($r1.cites.Count)"

# 4) lượt 2 — từ hồi chiếu "đó", client vẫn KHÔNG gửi history: ngữ cảnh bắt
# buộc phải đến từ message đã lưu trong DB thì model mới hiểu nó hỏi tài liệu nào.
$ask2 = 'Vậy tài liệu đó có mấy trang?'
$r2 = (Post-Json "$b/chat" @{ message = $ask2; conversation_id = $id; limit = 3 } $h 600).data
Write-Output '=== turn 2 answer (500 chars đầu) ==='
Write-Output $r2.answer.Substring(0, [Math]::Min(500, $r2.answer.Length))

# 5) sidebar: phiên phải hiện lên với title tự điền + số message
Show 'list conversations' ((Invoke-RestMethod -Uri "$b/conversations?limit=5" -Method Get -Headers $h).data)

# 6) lịch sử phiên đọc lại từ DB
$msgs = (Invoke-RestMethod -Uri "$b/conversations/$id/messages?limit=20" -Method Get -Headers $h).data
Write-Output "=== messages: $($msgs.Count) dòng ==="
foreach ($m in $msgs) {
  Write-Output ("[{0}] {1} | cites={2} | scope={3} | {4}" -f $m.id, $m.role, @($m.cites).Count,
    ($m.effective_category_ids -join ','), $m.content.Substring(0, [Math]::Min(70, $m.content.Length)))
}

# 7) PATCH đổi tên rồi đọc lại
function Patch-Json($uri, $obj, $headers) {
  $bytes = [Text.Encoding]::UTF8.GetBytes(($obj | ConvertTo-Json -Depth 6))
  return Invoke-RestMethod -Uri $uri -Method Patch -Headers $headers -ContentType 'application/json; charset=utf-8' `
    -Body $bytes -TimeoutSec 30
}
Show 'rename' ((Patch-Json "$b/conversations/$id" @{ title = 'Đổi tên bằng PATCH E2E' } $h).data)

# 8) title chỉ có khoảng trắng phải 400
try {
  Patch-Json "$b/conversations/$id" @{ title = '   ' } $h | Out-Null
  Write-Output 'SAI: title rỗng không bị chặn'
} catch { Write-Output "title rỗng -> $($_.Exception.Response.StatusCode.value__) (muốn 400)" }

# 9) đọc phiên của người khác phải 404 (không phải 403 — 403 lộ việc phiên tồn tại)
$tokOther = (Post-Json "$b/auth/login" @{ username = 'd24e26222'; password = 'secret123' } @{} 30).data.token
try {
  Invoke-RestMethod -Uri "$b/conversations/$id" -Method Get -Headers @{ Authorization = "Bearer $tokOther" } | Out-Null
  Write-Output 'SAI: đọc được phiên của người khác'
} catch { Write-Output "GET phiên người lạ -> $($_.Exception.Response.StatusCode.value__) (muốn 404)" }

# 10) gửi conversation_id không tồn tại vào /chat phải 404
try {
  Post-Json "$b/chat" @{ message = 'test'; conversation_id = 999999999 } $h 60 | Out-Null
  Write-Output 'SAI: conversation_id ma không bị chặn'
} catch { Write-Output "chat với phiên ma -> $($_.Exception.Response.StatusCode.value__) (muốn 404)" }

# 11) xoá mềm rồi đọc lại phải 404, message vẫn còn trong DB (purge để đợt sau)
Show 'delete' ((Invoke-RestMethod -Uri "$b/conversations/$id" -Method Delete -Headers $h).data)
try {
  Invoke-RestMethod -Uri "$b/conversations/$id" -Method Get -Headers $h | Out-Null
  Write-Output 'SAI: phiên đã xoá mà vẫn đọc được'
} catch { Write-Output "GET sau xoá -> $($_.Exception.Response.StatusCode.value__) (muốn 404)" }
