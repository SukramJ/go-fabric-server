# The python-client compatibility job

`compat-python` (`.github/workflows/ci.yml`, required for merge from v0.3.0
on) runs matterjs-server's python client at the version Home Assistant's
Matter integration pins — `matter-python-client==1.4.0` (core `2026.10.3`,
`manifest.json`) — against the built daemon:

1. `test/compat/requirements.txt` (generated from `requirements.in` with
   `pip-compile --generate-hashes`) is installed with `--require-hashes`
   into a fresh virtual environment;
2. `test/compat/run.sh` builds `fabric-server`, starts it with a temporary
   store on a free loopback port (`--enable-test-net-dcl`), and with
   `COMPAT_BRIDGE=1` builds go-fabric's `examples/reference-bridge` at the
   go-fabric version of `go.mod` from the module cache and starts it as a
   commissionable device;
3. `test/compat/test_python_client.py` connects as the integration's
   adapter does and checks `server_info` (schema 14, minimum 11),
   `start_listening`, `get_nodes`, `get_vendor_names`,
   `set_default_fabric_label`, `discover`, `parse_onboarding_payload` (the
   documented examples), `open_commissioning_window` on a missing node (the
   error class), an unimplemented command, the event subscription
   (`server_info_updated`), and — with the bridge — `commission_on_network`,
   `get_node`, `get_node_ip_addresses`, `ping_node`, `interview_node`,
   `get_matter_fabrics`, `get_network_topology`, `get_icd_state`,
   `open_commissioning_window` and `remove_node`.

The 1.4.0 client is a schema 11 client: it has no methods for the commands
of schemas 13 and 14 (`parse_onboarding_payload`, `batched_device_commands`,
the subscription commands) and its `commission_on_network` takes no filter;
the test sends those through the client's generic `send_command`.

Locally (Python 3.12 or later, a virtual environment outside the
repository):

```sh
python3 -m venv /tmp/compat && /tmp/compat/bin/pip install --require-hashes -r test/compat/requirements.txt
COMPAT_BRIDGE=1 PYTHON=/tmp/compat/bin/python make compat-python
```
