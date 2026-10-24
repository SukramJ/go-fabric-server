# SPDX-License-Identifier: Apache-2.0
# Copyright (C) 2026 SukramJ.
#
# Extracts the JSON examples of matterjs-server's docs/websockets_api.md into
# protocol/testdata/compat (see the README there). Run with python3 -I on a
# checkout of matterjs-server; it reads the file as data and runs nothing of it:
#   python3 -I script/extract-compat-fixtures.py <clone>/docs/websockets_api.md protocol/testdata/compat

import json, re, sys
src = sys.argv[1]
lines = open(src, encoding='utf-8').read().split('\n')
blocks = []
heading = ''
i = 0
while i < len(lines):
    l = lines[i]
    m = re.match(r'^\*\*([a-z_]+)\*\*', l)
    if m: heading = m.group(1)
    h = re.match(r'^#+ (.*)', l)
    if h: heading = re.sub(r'[^a-z0-9]+','_',h.group(1).lower()).strip('_')
    if l.strip() == '```json':
        start = i+1
        j = start
        while lines[j].strip() != '```': j += 1
        body = '\n'.join(lines[start:j])
        try:
            json.loads(body); ok = True
        except Exception:
            ok = False
        blocks.append((start, heading, ok, body))
        i = j
    i += 1
for b in blocks:
    print(b[0], b[1], 'OK' if b[2] else 'invalid', b[3].replace('\n',' ')[:90])

if len(sys.argv) > 2:
    import os
    out = sys.argv[2]
    os.makedirs(out, exist_ok=True)
    seen = {}
    for (line, heading, ok, body) in blocks:
        if not ok:
            if heading == 'node_added':
                body = body.replace('"attributes": { ... }', '"attributes": {}')
                json.loads(body)
            else:
                continue
        obj = json.loads(body)
        kind = 'request' if isinstance(obj, dict) and 'command' in obj else ('event' if isinstance(obj, dict) and 'event' in obj else ('response' if isinstance(obj, dict) and 'message_id' in obj else 'value'))
        name = f"{heading}.{kind}"
        seen[name] = seen.get(name, 0) + 1
        if seen[name] > 1:
            name += f".{seen[name]}"
        with open(os.path.join(out, name + '.json'), 'w', encoding='utf-8') as f:
            f.write(body.strip() + '\n')
        print(line, name)
