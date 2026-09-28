#!/bin/sh
cd /opt/savvy/deploy/data/zeroclaw/.zeroclaw/data/state 2>/dev/null || exit 1
python3 - <<'PY'
import json, re
evts = []
with open('runtime-trace.jsonl', 'r', errors='replace') as f:
    for line in f:
        try:
            o = json.loads(line)
        except Exception:
            continue
        s = json.dumps(o, ensure_ascii=False)
        if 'payapp' in s:
            evts.append((o.get('@timestamp'), o, s))

print("### 全部出现过的 sid 值(去重, 含来源)")
allsids = {}
for ts, o, s in evts:
    for m in re.finditer(r'sid=([0-9a-fA-F\-]+)', s):
        allsids.setdefault(m.group(1), []).append(ts)
for k, v in allsids.items():
    print("  sid=", k, " len=", len(k), " 出现:", len(v), "首末:", v[-1], v[0])

print("### 最后一条 payapp 回复全文")
ts, o, s = evts[-1]
a = o.get('attributes') or {}
print("ts:", ts, "keys(attrs):", sorted(a.keys()))
txt = a.get('text') or ''
if not txt:
    rr = a.get('raw_response') or ''
    try:
        j = json.loads(rr)
        txt = j['choices'][0]['message']['content']
    except Exception:
        txt = rr
print("----回复正文----")
print(txt[:1200])
print("----是否含 weixin://----", 'weixin://' in txt)
PY