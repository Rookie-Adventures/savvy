# -*- coding: utf-8 -*-
"""SSH diagnose - try multiple passwords"""
import paramiko, sys

if sys.stdout.encoding != 'utf-8':
    sys.stdout.reconfigure(encoding='utf-8', errors='replace')

HOST_A = "8.135.58.63"
HOST_B = "120.77.11.137"
PASSWORDS = ["Aa123123..", "Qq123123.."]

def ssh_run(host, password, cmd, timeout=15):
    ssh = paramiko.SSHClient()
    ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
    ssh.connect(host, username="root", password=password, timeout=timeout)
    stdin, stdout, stderr = ssh.exec_command(cmd, timeout=30)
    out = stdout.read().decode('utf-8', errors='replace').strip()
    err = stderr.read().decode('utf-8', errors='replace').strip()
    ssh.close()
    return out, err

# Try Machine A with both passwords
for pwd in PASSWORDS:
    try:
        print(f"=== Machine A (pwd: {pwd[:2]}...) ===")
        out, err = ssh_run(HOST_A, pwd, "echo 'SSH OK' && systemctl is-active nginx && df -h / | tail -1 && free -h | grep Mem")
        print(out)
        break
    except Exception as e:
        print(f"  FAILED: {e}")

# Try Machine B
print()
for pwd in PASSWORDS:
    try:
        print(f"=== Machine B (pwd: {pwd[:2]}...) ===")
        out, err = ssh_run(HOST_B, pwd, "echo 'SSH OK' && docker ps -a --format 'table {{.Names}}\t{{.Status}}' && echo '---' && df -h / | tail -1 && free -h | grep Mem")
        print(out)
        
        print("\n=== Machine B: new-api logs ===")
        out, err = ssh_run(HOST_B, pwd, "docker logs --tail 20 new-api 2>&1")
        print(out or err)
        
        print("\n=== Machine B: curl new-api ===")
        out, err = ssh_run(HOST_B, pwd, "curl -s -o /dev/null -w '%{http_code}' --max-time 5 http://localhost:3000/")
        print(f"HTTP {out}")
        break
    except Exception as e:
        print(f"  FAILED: {e}")
