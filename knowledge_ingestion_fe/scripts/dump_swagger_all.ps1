# Dump toàn bộ endpoint + definition từ swagger của backend đang chạy
$j = Invoke-RestMethod -Uri 'http://localhost:9001/swagger/doc.json'

'===== ENDPOINTS ====='
foreach ($p in $j.paths.PSObject.Properties) {
  foreach ($m in $p.Value.PSObject.Properties) {
    $op = $m.Value
    $params = ($op.parameters | ForEach-Object { "$($_.name)($($_.in),$($_.required))" }) -join ','
    $resp = ($op.responses.PSObject.Properties.Name) -join '/'
    '{0,-6} {1,-42} | tags={2} | summ={3} | params={4} | resp={5}' -f $m.Name.ToUpper(), $p.Name, ($op.tags -join ','), $op.summary, $params, $resp
  }
}

'===== DEFINITIONS (fields) ====='
foreach ($d in $j.definitions.PSObject.Properties) {
  $props = ($d.Value.properties.PSObject.Properties | ForEach-Object { "$($_.Name):$($_.type)" }) -join ', '
  '{0} :: {1}' -f $d.Name, $props
}
