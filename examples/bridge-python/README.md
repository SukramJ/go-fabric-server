# A bridge host in Python

`bridge.py` is a host of go-fabric-server's bridge half (`/bridge`) in 71
lines of Python: it declares one lamp (an OnOffLight whose Toggle it
answers itself), prints the pairing codes, answers each `command` event
before its deadline, `set`s the lamp's new state and prints what a
controller changed. It shows the protocol to a non-Go host; it is not run
in CI.

```sh
fabric-server --controller=false --bridge --listen 127.0.0.1:5580 --storage-path ./data
pip install websockets
FABRIC_SERVER_URL=ws://127.0.0.1:5580/bridge python3 bridge.py
```

Pair the printed code with a controller (Apple Home, Google Home, Home
Assistant); toggling the lamp there reaches `on_event` as a `command`
event. With `--token`, set `FABRIC_SERVER_TOKEN`. Every message is in
[`docs/protocol.md`](../../docs/protocol.md), section "The bridge half".
