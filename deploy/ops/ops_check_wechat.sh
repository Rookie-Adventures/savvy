#!/bin/bash
DB=$(docker ps --format '{{.Names}}' | grep -iE 'db|postgres' | head -1)
U=$(docker exec "$DB" env | grep '^POSTGRES_USER=' | cut -d= -f2)
D=$(docker exec "$DB" env | grep '^POSTGRES_DB=' | cut -d= -f2)
docker exec "$DB" psql -U "$U" -d "$D" -tAc "select key || ' | len=' || length(value) || ' | ' || case when key in ('WechatPrivateKeyPEM','WechatPayPublicKey','AlipayAppPrivateKey','AlipayPublicKey') then left(value,27) when key='WechatAPIv3Key' then left(value,4) || '....' else value end from options where key ~* '^wechat' order by key"
