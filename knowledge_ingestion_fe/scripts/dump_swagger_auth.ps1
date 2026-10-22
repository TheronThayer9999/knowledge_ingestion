$ErrorActionPreference = 'Stop'
$r = Invoke-RestMethod -Uri 'http://localhost:9001/swagger/doc.json'
$authPaths = '/api/v1/auth/login', '/api/v1/auth/refresh', '/api/v1/auth/logout', '/api/v1/users', '/api/v1/auth/profile'
foreach ($p in $authPaths) {
  $node = $r.paths.$p
  if (-not $node) { Write-Output "== $p : NOT FOUND"; continue }
  foreach ($m in $node.PSObject.Properties.Name) {
    Write-Output "== $m.toUpperCase() $p"
    $op = $node.$m
    Write-Output ("   params: " + (($op.parameters | ForEach-Object { $_.name + '(' + $_.in + ' ' + $_.type + ')' }) -join ', '))
    Write-Output ("   schema-ref: " + (($op.responses.PSObject.Properties | ForEach-Object { $_.Name + '->' + $_.Value.'$ref' }) -join ' '))
  }
}
Write-Output '== definitions (auth/user related):'
foreach ($d in $r.definitions.PSObject.Properties.Name) {
  if ($d -match 'Login|Refresh|Logout|UserResponse|CreateUser|ResponseResource') {
    $props = $r.definitions.$d.properties
    Write-Output ("   $d : " + (($props.PSObject.Properties | ForEach-Object { $_.Name + ':' + $_.Value.type }) -join ', '))
  }
}
