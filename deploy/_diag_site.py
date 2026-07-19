# -*- coding: utf-8 -*-
"""Diagnose why main site is down"""
import subprocess, json, base64, time, os

REGION = "cn-shenzhen"
MACHINE_A = "i-wz953hq55nljkz6jwm21"
MACHINE_B = "i-wz9b5nhr3idgu8fqvnvk"

def get_env():
    env = os.environ.copy()
    mp = subprocess.run(['powershell', '-Command',
        '[System.Environment]::GetEnvironmentVariable("Path","Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path","User")'],
        capture_output=True, text=True).stdout.strip()
    env['Path'] = mp
    return env

def run_remote(instance_id, script, wait=10):
    b64 = base64.b64encode(script.encode()).decode()
    env = get_env()
    r = subprocess.run(
        ['aliyun', 'ecs', 'RunCommand', '--RegionId', REGION,
         '--Type', 'RunShellScript', '--CommandContent', b64,
         '--InstanceId.1', instance_id, '--ContentEncoding', 'Base64'],
        capture_output=True, env=env
    )
    d = json.loads(r.stdout.decode('utf-8'))
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
    return base64.b64decode(
        d2['Invocation']['InvocationResults']['InvocationResult'][0].get('Output', '')
    ).decode('utf-8', 'replace')

# Machine A: nginx status + connectivity test
print("=== Machine A: nginx status ===")
print(run_remote(MACHINE_A, "systemctl status nginx --no-pager -l 2>&1 | head -20", wait=8))

print("\n=== Machine A: disk/memory ===")
print(run_remote(MACHINE_A, "df -h / && echo && free -h", wait=8))

print("\n=== Machine A: curl localhost:3000 (via Machine B) ===")
print(run_remote(MACHINE_A, "curl -s -o /dev/null -w '%{http_code}' http://172.24.96.233:3000/ --max-time 5 2>&1", wait=8))

# Machine B: container status
print("\n=== Machine B: docker ps ===")
print(run_remote(MACHINE_B, "docker ps -a --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}' 2>&1", wait=8))

print("\n=== Machine B: new-api logs (last 30 lines) ===")
print(run_remote(MACHINE_B, "docker logs --tail 30 new-api 2>&1", wait=8))

print("\n=== Machine B: disk/memory ===")
print(run_remote(MACHINE_B, "df -h / && echo && free -h", wait=8))
