#!/usr/bin/env bash
# SQLite 热备份 + 滚动归档 (定时跑在机B)
#
# 部署: sudo cp backup.sh /usr/local/bin/savvy-backup.sh && sudo chmod +x /usr/local/bin/savvy-backup.sh
#       sudo crontab -e  → 加: 0 3 * * * /usr/local/bin/savvy-backup.sh >> /var/log/savvy-backup.log 2>&1
#       (每天凌晨 3 点跑)
#
# 策略: 本地留 7 天, 7 天以上删除; 可选 rclone 推到对象存储异地保存

set -euo pipefail

# ---- 配置 ----
DATA_DIR="/opt/savvy/deploy/data"              # docker-compose 里 ./data 宿主绝对路径
BACKUP_DIR="/var/backups/savvy"                 # 本地备份目录
KEEP_DAYS=7
DB_FILES=(
  "new-api/one-api.db"      # new-api 主库
  "savvy-manager/savvy-manager.db"
)
RCLONE_REMOTE=""            # 留空=不推对象存储; 填 myoss:savvy-backups 启用异地
DATE=$(date +%Y%m%d_%H%M%S)

mkdir -p "$BACKUP_DIR"

echo "[$DATE] === Savvy backup start ==="

for rel in "${DB_FILES[@]}"; do
  src="$DATA_DIR/$rel"
  name=$(basename "$rel" .db)
  if [ ! -f "$src" ]; then
    echo "[skip] $src not found"
    continue
  fi
  dst="$BACKUP_DIR/${name}_${DATE}.db"

  # SQLite 热备份: 用 .backup 命令而非 cp, 避免写中半文件 (WAL一致)
  # ponytail: 容器在写时 .backup 比直接 cp 安全, 这是 SQLite 官方推荐
  sqlite3 "$src" ".backup '$dst'"
  # 压缩省空间
  gzip "$dst"

  echo "[ok] $rel -> $dst.gz"

  # 异地推送 (可选)
  if [ -n "$RCLONE_REMOTE" ]; then
    rclone copy "$dst.gz" "$RCLONE_REMOTE/$(hostname)/${name}/" 2>/dev/null \
      && echo "[push] $dst.gz -> $RCLONE_REMOTE" \
      || echo "[warn] rclone push failed (继续本地保留)"
  fi
done

# 滚动清理: 删本地 KEEP_DAYS 天前的备份
find "$BACKUP_DIR" -name "*.db.gz" -mtime +$KEEP_DAYS -delete
echo "[clean] removed backups older than ${KEEP_DAYS}d"

echo "[$(date +%Y%m%d_%H%M%S)] === Savvy backup done ==="

# ponytail: SQLite 单文件适合这套, 迁 MySQL 后改 mysqldump
