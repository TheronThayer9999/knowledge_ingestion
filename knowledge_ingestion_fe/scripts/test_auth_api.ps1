$ErrorActionPreference = 'Continue'
$base = 'http://localhost:9001/api/v1'

function CallApi($method, $path, $body, $token) {
  $uri = "$base$path"
  $headers = @{}
  if ($token) { $headers['Authorization'] = "Bearer $token" }
  try {
    if ($body) {
      $json = $body | ConvertTo-Json -Depth 6
      $resp = Invoke-WebRequest -UseBasicParsing -Method $method -Uri $uri -Body $json -ContentType 'application/json' -Headers $headers
    } else {
      $resp = Invoke-WebRequest -UseBasicParsing -Method $method -Uri $uri -Headers $headers
    }
    return @{ status = [int]$resp.StatusCode; body = $resp.Content }
  } catch {
    $r = $_.Exception.Response
    if ($r) {
      $reader = New-Object System.IO.StreamReader($r.GetResponseStream())
      $content = $reader.ReadToEnd()
      return @{ status = [int]$r.StatusCode; body = $content }
    }
    return @{ status = -1; body = $_.Exception.Message }
  }
}

Write-Output '--- 1. login sai mat khau:'
(1,2 | ForEach-Object { $x = CallApi POST '/auth/login' @{username='feuser';password='wrongpass1'}; "$($x.status) => $($x.body)" })

Write-Output '--- 2. tao user feuser:'
$x = CallApi POST '/users' @{username='feuser'; password='feuser12345'; email='feuser@example.com'}
"$($x.status) => $($x.body)"

Write-Output '--- 3. tao trung ten feuser:'
$x = CallApi POST '/users' @{username='feuser'; password='feuser12345'; email='feuser@example.com'}
"$($x.status) => $($x.body)"

Write-Output '--- 4. login feuser dung:'
$x = CallApi POST '/auth/login' @{username='feuser'; password='feuser12345'}
"$($x.status) => $($x.body)"
$login = $x.body | ConvertFrom-Json
$token = $login.data.token
$refresh = $login.data.refresh_token

Write-Output '--- 5. profile voi token:'
$x = CallApi GET '/auth/profile' $null $token
"$($x.status) => $($x.body)"

Write-Output '--- 6. refresh doi cap token:'
$x = CallApi POST '/auth/refresh' @{refresh_token=$refresh}
"$($x.status) => $($x.body)"
$newRefresh = ($x.body | ConvertFrom-Json).data.refresh_token

Write-Output '--- 7. dung lai refresh token cu (reuse detection):'
$x = CallApi POST '/auth/refresh' @{refresh_token=$refresh}
"$($x.status) => $($x.body)"

Write-Output '--- 8. logout bang refresh moi:'
$x = CallApi POST '/auth/logout' @{refresh_token=$newRefresh}
"$($x.status) => $($x.body)"
