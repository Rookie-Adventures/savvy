# 机A SSH 入侵与挖矿木马事件

> **日期**: 2026-07-15 ~ 2026-07-19
> **状态**: 已清除、已加固
> **影响范围**: 仅机A（`i-wz953hq55nljkz6jwm21`），机B 未受影响

## 症状

- 机A nginx 反复被 SIGKILL 杀掉，手动重启后数小时再次被杀
- 第一次：7月15日 03:05；第二次：7月19日 08:05
- 收到阿里云安全告警
- 主站 `scheng.net` 间歇性无法访问

## 根因

机A 通过 SSH 暴力破解被入侵，植入了 **kswapd0 挖矿木马**。

### 入侵证据

| 恶意文件 | 作用 |
|---|---|
| `/root/.configrc7/a/kswapd00` (2.2MB) | 挖矿主程序，伪装内核 kswapd 进程 |
| `/root/.configrc7/a/delsshd` | 清除 SSH 登录日志 |
| `/root/.configrc7/a/rmkwork` | 清除 kworker 痕迹 |
| `/root/.configrc7/a/init01`, `init02` | 持久化脚本 |
| `/tmp/.X291-unix/dota3.tar.gz` | 原始投递 payload |
| `/tmp/.X291-unix/.rsync/c/kthreadadd32/64` | 横向扫描传播工具 |
| `/tmp/.X291-unix/.rsync/c/aptitude` | 伪装 aptitude 的定时任务 |

### 恶意 cron 条目（5 条）

```
5 6 */2 * 0  /root/.configrc7/a/upd     # 每 2 天周日拉起挖矿
@reboot      /root/.configrc7/a/upd      # 重启自动拉起
5 8 * * 0    /root/.configrc7/b/sync     # 每周日同步
@reboot      /root/.configrc7/b/sync     # 重启自动同步
0 0 */3 * *  /tmp/.X291-unix/.rsync/c/aptitude  # 每 3 天扫描传播
```

### nginx 被杀的直接原因

7月19日 08:05:01 journalctl 记录：

```
CRON: (root) CMD (/root/.configrc7/b/sync)
systemd: nginx.service: Main process exited, code=killed, status=9/KILL
systemd: ssh.service: Main process exited, code=killed, status=9/KILL
```

恶意 `sync` 脚本执行时连带 SIGKILL 了 nginx 和 SSH 进程。

### 暴力破解证据

`lastb` 显示大量失败登录尝试：

```
ftpuser  14.103.118.114   (尝试 ftpuser/root)
farid    218.94.137.166   (尝试 farid/polls)
pro      14.103.118.114   (尝试 pro)
```

## 清除步骤

1. `pkill -f kswapd00` + 相关恶意进程
2. `rm -rf /root/.configrc7 /tmp/.X291-unix`
3. 清除 5 条恶意 cron 条目
4. 验证：目录不存在，crontab 为空

## 安全加固

| 措施 | 配置 |
|---|---|
| **nginx 自动重启** | systemd override: `Restart=always, RestartSec=5` |
| **看门狗 cron** | `/usr/local/bin/nginx-watchdog.sh` 每 2 分钟检查 nginx + 内存 |
| **fail2ban** | SSH 5 次失败 → 封 IP 1 小时 (`maxretry=5, bantime=3600`) |
| **SSH 加固** | `PermitRootLogin prohibit-password`, `MaxAuthTries 3` |
| **密码重置** | root 密码已重置（不记录在文档中） |
| **2G Swap** | `/swapfile`, `swappiness=10`, 写入 `/etc/fstab` 持久化 |

## 机B 确认

- 无恶意 cron
- 无 `/root/.configrc7` 或 `/tmp/.X291-unix`
- 无可疑进程
- 所有容器正常运行

## 教训

1. **机A 2C/2G 跑 100 个 SSL server block** 内存余量极小，此前怀疑 OOM 但实际是木马杀的
2. SSH 密码认证 + 公网暴露 = 暴力破解高风险，应从一开始就用密钥登录 + fail2ban
3. systemd 默认 `Restart=no`，服务被杀后不会自动恢复，关键服务必须配 `Restart=always`
4. 阿里云安全告警要及时处理，不能等网站挂了才发现
