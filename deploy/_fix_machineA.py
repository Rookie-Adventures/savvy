# -*- coding: utf-8 -*-
"""Restart nginx on Machine A + reset SSH password"""
import subprocess, json, base64, time, os

REGION = "cn-shenzhen"
MACHINE_A = "i-wz953hq55nljkz6jwm21"

def get_env():
    env = os.environ.copy()
    mp = subprocess.run(['powershell', '-Command',
        '[System.Environment]::GetEnvironmentVariable("Path","Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path","User")'],
        capture_output=True, text=True).stdout.strip()
    env['Path'] = mp
    return env

def run_remote(script, wait=10):
    b64 = base64.b64encode(script.encode()).decode()
    env = get_env()
    r = subprocess.run(
        ['aliyun', 'ecs', 'RunCommand', '--RegionId', REGION,
         '--Type', 'RunShellScript', '--CommandContent', b64,
         '--InstanceId.1', MACHINE_A, '--ContentEncoding', 'Base64'],
        capture_output=True, env=env
    )
    raw = r.stdout.decode('utf-8')
    if not raw.strip():
        return "aliyun empty"
    try:
        d = json.loads(raw)
    except:
        return f"non-JSON: {raw[:200]}"
    inv = d.get('InvokeId')
    if not inv:
        return f"ERROR: {d}"
    time.sleep(wait)
    r2 = subprocess.run(
        ['aliyun', 'ecs', 'DescribeInvocationResults', '--RegionId', REGION, '--InvokeId', inv],
        capture_output=True, env=env
    )
    d2 = json.loads(r2.stdout.decode('utf-8'))
    result = d2['Invocation']['InvocationResults']['InvocationResult'][0]
    output = base64.b64decode(result.get('Output', '')).decode('utf-8', 'replace')
    return output

# Step 1: Reset root password to Aa123123..
print("=== Reset root password ===")
print(run_remote("echo 'root:Aa123123..' | chpasswd && echo 'Password reset OK'", wait=5))

# Step 2: Restart nginx
print("\n=== Restart nginx ===")
print(run_remote("""
systemctl restart nginx 2>&1
sleep 2
systemctl is-active nginx
echo "---"
curl -sk -o /dev/null -w 'HTTPS localhost: %{http_code}' --max-time 5 https://localhost/
echo ""
curl -sk -o /dev/null -w 'HTTPS scheng.net: %{http_code}' --max-time 5 https://scheng.net/
""", wait=12))
