$ErrorActionPreference = 'Stop'
$base = 'http://127.0.0.1:9001/api/v1'
$login = Invoke-RestMethod -Method Post -Uri "$base/auth/login" -ContentType 'application/json' -Body (@{ username = 'uismoke01'; password = 'secret123' } | ConvertTo-Json)
$headers = @{ Authorization = "Bearer $($login.data.token)" }

function NewFolder($name, $parentId) {
  $body = @{ name = $name }
  if ($null -ne $parentId) { $body.parent_id = $parentId }
  return (Invoke-RestMethod -Method Post -Uri "$base/categories" -Headers $headers -ContentType 'application/json' -Body ($body | ConvertTo-Json)).data
}

$a = NewFolder 'UI Verify A' $null
$b = NewFolder 'UI Verify B' $null
$c = NewFolder 'UI Verify Child' $a.id

$sql = "insert into articles (created_at, updated_at, url, name, content_type, description, category_id, user_id, storage_key, extract_status, index_status) select now(), now(), '', 'UI Verify Doc', 'text/plain', 'seed for ui check', $($a.id), id, 'verify/ui.txt', 'done', 'done' from users where user_name='uismoke01' returning id;"
docker exec -i theron_postgres psql -U theron_user -d manager_db -t -c $sql

Write-Host "seeded A=$($a.id) B=$($b.id) C=$($c.id)"
