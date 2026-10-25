# SPDX-License-Identifier: Apache-2.0
# Copyright (C) 2026 SukramJ.
"""A bridge host in Python: one lamp on go-fabric-server's /bridge half.

The host declares an OnOffLight whose Toggle it answers itself, prints the
pairing codes, answers each command event before its deadline and prints
what a controller changed. Needs `pip install websockets` and a daemon run
with --bridge. docs/protocol.md describes every message.
"""
import asyncio
import itertools
import json
import os

import websockets

URL = os.environ.get("FABRIC_SERVER_URL", "ws://127.0.0.1:5580/bridge")
TOKEN = os.environ.get("FABRIC_SERVER_TOKEN")
DECLARATION = {"devices": [{
    "key": "lamp-1", "type": "OnOffLight", "name": "Python lamp",
    "clusters": {"OnOff": {"initial": {"onOff": False}, "handle_commands": ["Toggle"]}},
}]}

ids = itertools.count(1)
lamp_on = False


async def main():
    headers = {"Authorization": f"Bearer {TOKEN}"} if TOKEN else {}
    async with websockets.connect(URL, additional_headers=headers) as ws:
        print("server:", json.loads(await ws.recv()))  # the bare server_info
        waiting = {}

        async def call(command, args=None):
            mid = str(next(ids))
            waiting[mid] = asyncio.get_running_loop().create_future()
            await ws.send(json.dumps({"message_id": mid, "command": command, "args": args or {}}))
            return await waiting[mid]

        async def on_event(name, data):
            global lamp_on
            if name == "command" and data["command"] == "Toggle":
                lamp_on = not lamp_on  # switch the real lamp here
                await call("result", {"correlation": data["correlation"], "ok": True})
                await call("set", {"device": data["device"], "cluster": "OnOff", "patch": {"onOff": lamp_on}})
            elif name == "changed":
                print("controller changed", data["device"], data["cluster"], data["state"], data["origin"])
            elif name == "result_timeout":
                print("missed a deadline:", data)

        async def reader():
            async for raw in ws:
                msg = json.loads(raw)
                if "event" in msg:
                    asyncio.create_task(on_event(msg["event"], msg["data"]))
                elif msg.get("message_id") in waiting:
                    fut = waiting.pop(msg["message_id"])
                    if "error_code" in msg:
                        fut.set_exception(RuntimeError(f'{msg["error_code"]}: {msg["details"]}'))
                    else:
                        fut.set_result(msg.get("result"))

        read = asyncio.create_task(reader())
        print("declared:", await call("declare", DECLARATION))
        print("pair with:", await call("pairing_codes"))
        await call("start_listening")
        await read


if __name__ == "__main__":
    asyncio.run(main())
