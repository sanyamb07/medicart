#!/usr/bin/env bash
# Nightly Postgres backup for the OneTrueDose production VPS.
#
# Dumps the `medicart` database from the running `medicart-db` container,
# compresses it, and uploads it to the same Cloudflare R2 bucket used for
# prescription storage (a separate prefix, so the two never collide) — R2
# has no egress fee, so pulling a backup back down to restore costs nothing.
# Old backups beyond RETENTION_DAYS are deleted, both locally and in R2.
#
# Install on the VPS:
#   crontab -e
#   0 3 * * * /opt/medicart/infra/scripts/backup-db.sh >> /var/log/medicart-backup.log 2>&1
#
# Requires on the host: `aws` CLI configured for R2 (see docs/DEPLOYMENT.md
# "Backups" section for the one-time `aws configure` + endpoint setup).
set -euo pipefail

RETENTION_DAYS="${BACKUP_RETENTION_DAYS:-14}"
BACKUP_DIR="${BACKUP_DIR:-/opt/medicart/backups}"
R2_BUCKET="${R2_BACKUP_BUCKET:?set R2_BACKUP_BUCKET, e.g. onetruedose-backups}"
R2_ENDPOINT="${R2_ENDPOINT:?set R2_ENDPOINT, e.g. https://<account-id>.r2.cloudflarestorage.com}"

timestamp="$(date -u +%Y%m%d-%H%M%S)"
filename="medicart-${timestamp}.sql.gz"
mkdir -p "$BACKUP_DIR"

echo "[$(date -u +%FT%TZ)] starting backup -> $filename"

docker exec medicart-db pg_dump -U medicart -d medicart --no-owner --no-privileges \
  | gzip -9 > "$BACKUP_DIR/$filename"

size=$(du -h "$BACKUP_DIR/$filename" | cut -f1)
echo "[$(date -u +%FT%TZ)] dump complete: $size"

aws s3 cp "$BACKUP_DIR/$filename" "s3://${R2_BUCKET}/db-backups/$filename" \
  --endpoint-url "$R2_ENDPOINT"

echo "[$(date -u +%FT%TZ)] uploaded to r2://${R2_BUCKET}/db-backups/$filename"

# Local retention: delete dumps older than RETENTION_DAYS.
find "$BACKUP_DIR" -name 'medicart-*.sql.gz' -mtime "+${RETENTION_DAYS}" -delete

# Remote retention: same window, in R2.
cutoff_epoch=$(date -u -d "-${RETENTION_DAYS} days" +%s)
aws s3 ls "s3://${R2_BUCKET}/db-backups/" --endpoint-url "$R2_ENDPOINT" | while read -r line; do
  obj_date=$(echo "$line" | awk '{print $1" "$2}')
  obj_key=$(echo "$line" | awk '{print $4}')
  [ -z "$obj_key" ] && continue
  obj_epoch=$(date -u -d "$obj_date" +%s 2>/dev/null || echo 0)
  if [ "$obj_epoch" -lt "$cutoff_epoch" ] && [ "$obj_epoch" -gt 0 ]; then
    echo "[$(date -u +%FT%TZ)] pruning old backup: $obj_key"
    aws s3 rm "s3://${R2_BUCKET}/db-backups/$obj_key" --endpoint-url "$R2_ENDPOINT"
  fi
done

echo "[$(date -u +%FT%TZ)] backup complete"
