# The python-client compatibility job

`compat-python` (`.github/workflows/ci.yml`, required for merge from v0.3.0
on) runs matterjs-server's python client at the version Home Assistant's
Matter integration pins — `matter-python-client==1.5.5` (core `2026.11.0`,
`manifest.json:13`, the first release with the integration's camera
platform) — against the built daemon:

1. `test/compat/requirements.txt` (generated from `requirements.in` with
   `uv pip compile --generate-hashes --python-version 3.13 --no-header`) is installed with `--require-hashes`
   into a fresh virtual environment;
2. `test/compat/run.sh` builds `fabric-server`, starts it with a temporary
   store on a free loopback port (`--enable-test-net-dcl`), and with
   `COMPAT_BRIDGE=1` builds go-fabric's `examples/reference-bridge` at the
   go-fabric version of `go.mod` from the module cache and starts it as a
   commissionable device with its simulated camera (`--camera-sim
   --camera-test-videosrc`) on TCP (`--tcp true`);
3. `test/compat/test_python_client.py` connects as the integration's
   adapter does and checks `server_info` (schema 14, minimum 11),
   `start_listening`, `get_nodes`, `get_vendor_names`,
   `set_default_fabric_label`, `discover`, `parse_onboarding_payload` (the
   documented examples), `open_commissioning_window` on a missing node (the
   error class), `camera_get_capabilities` on a missing node, an
   unimplemented command, the event subscription (`server_info_updated`),
   and — with the bridge — `commission_on_network`, `get_node`,
   `get_node_ip_addresses`, `ping_node`, `interview_node`,
   `get_matter_fabrics`, `get_network_topology`, `get_icd_state`, the
   camera's `camera_get_capabilities`, `camera_snapshot` (JPEG),
   `camera_start_stream` (an offer and a `client_session_key`) and
   `camera_stop_stream` by the key, `open_commissioning_window` and
   `remove_node`.

The test calls the camera commands through the 1.5.5 client's own methods;
`parse_onboarding_payload` and `commission_on_network` (whose raw node data
the camera leg reads) go through its generic `send_command`.

Locally (Python 3.12 or later, a virtual environment outside the
repository):

```sh
python3 -m venv /tmp/compat && /tmp/compat/bin/pip install --require-hashes -r test/compat/requirements.txt
COMPAT_BRIDGE=1 PYTHON=/tmp/compat/bin/python make compat-python
```
