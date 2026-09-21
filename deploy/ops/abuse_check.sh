DB=$(docker ps --format "{{.Names}}" | grep -iE "db|postgres" | head -1)
U=$(docker exec $DB env | grep ^POSTGRES_USER= | cut -d= -f2)
D=$(docker exec $DB env | grep ^POSTGRES_DB= | cut -d= -f2)
echo ---REG_TODAY
docker exec $DB psql -U $U -d $D -c "select id, username, display_name, created_at from users where created_at >= 1789574420 order by id desc limit 15"
echo ---CONSUME_BY_USER_TODAY
docker exec $DB psql -U $U -d $D -c "select user_id, count(*) as calls, sum(quota) as quota_used from logs where type=2 and created_at >= 1789574420 group by user_id order by calls desc limit 10"
echo ---TOP_IP_24H
docker logs new-api --since 24h 2>&1 | grep "2026/09/1[67]" | awk "{print \$(NF-3)}" | sort | uniq -c | sort -rn | head -8
