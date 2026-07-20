# -*- coding: utf-8 -*-
"""Add 2GB swap to Machine A"""
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

print("=== Add 2GB swap ===")
print(run_remote("""
# Check existing swap
echo "--- Before ---"
free -h | grep -E 'Mem|Swap'

# Check if swap already exists
if [ -f /swapfile ]; then
    echo "Swap file already exists, skipping creation"
else
    echo "Creating 2GB swap file..."
    fallocate -l 2G /swapfile
    chmod 600 /swapfile
    mkswap /swapfile
    swapon /swapfile
    echo "Swap created and enabled"
fi

# Make persistent across reboots
grep -q '/swapfile' /etc/fstab || echo '/swapfile none swap sw 0 0' >> /etc/fstab

# Tune swappiness (use swap only when needed)
sysctl vm.swappiness=10
grep -q 'vm.swappiness' /etc/sysctl.conf && sed -i 's/vm.swappiness=.*/vm.swappiness=10/' /etc/sysctl.conf || echo 'vm.swappiness=10' >> /etc/sysctl.conf

echo ""
echo "--- After ---"
free -h | grep -E 'Mem|Swap'
cat /proc/swaps
""", wait=15))
