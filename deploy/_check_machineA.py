# -*- coding: utf-8 -*-
"""Check Machine A via aliyun RunCommand (bypass SSH)"""
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
        print(f"aliyun CLI returned empty. stderr: {r.stderr.decode('utf-8','replace')}")
        return ""
    try:
        d = json.loads(raw)
    except:
        print(f"aliyun CLI non-JSON: {raw[:200]}")
        return ""
    inv = d.get('InvokeId')
    if not inv:
        print(f"ERROR: {d}")
        return ""
    time.sleep(wait)
    r2 = subprocess.run(
        ['aliyun', 'ecs', 'DescribeInvocationResults', '--RegionId', REGION, '--InvokeId', inv],
        capture_output=True, env=env
    )
    d2 = json.loads(r2.stdout.decode('utf-8'))
    result = d2['Invocation']['InvocationResults']['InvocationResult'][0]
    output = base64.b64decode(result.get('Output', '')).decode('utf-8', 'replace')
    status = result['InvocationStatus']
    return f"[{status}] {output}"

print("=== Machine A: nginx + system ===")
print(run_remote("""
echo "--- nginx status ---"
systemctl is-active nginx
systemctl status nginx --no-pager 2>&1 | head -10

echo "--- disk ---"
df -h / | tail -1

echo "--- memory ---"
free -h | grep Mem

echo "--- curl B:3000 ---"
curl -s -o /dev/null -w 'HTTP %{http_code}' --max-time 5 http://172.24.96.233:3000/ 2>&1

echo ""
echo "--- curl localhost:443 ---"
curl -sk -o /dev/null -w 'HTTPS %{http_code}' --max-time 5 https://localhost/ 2>&1
""", wait=12))
