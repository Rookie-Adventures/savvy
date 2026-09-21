param(
  [string]$File,
  [string]$Instance,
  [int]$Timeout = 90,
  [int]$Wait = 8
)
$txt = (Get-Content -Raw $File) -replace "`r`n", "`n"
$b64 = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($txt))
$r = aliyun ecs RunCommand --RegionId cn-shenzhen --Type RunShellScript --CommandContent $b64 --ContentEncoding Base64 --InstanceId.1 $Instance --Timeout $Timeout | ConvertFrom-Json
Start-Sleep -Seconds $Wait
$res = aliyun ecs DescribeInvocationResults --RegionId cn-shenzhen --InvokeId $r.InvokeId | ConvertFrom-Json
$iv = $res.Invocation.InvocationResults.InvocationResult[0]
echo "STATUS=$($iv.InvocationStatus)"
[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($iv.Output))
