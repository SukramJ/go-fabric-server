# Compatibility fixtures in the python client's shape

The results, events and errors of the commands S3 added that
matterjs-server's `docs/websockets_api.md` has no example of (or only a
partial one), recorded in the shape of matterjs-server's python client
(`python_client/matter_server/common/models.py` at
`0bd06f222a3d51415980022c5f207705de57b8aa`, the source of
`matter-python-client`, which Home Assistant pins at 1.4.0):

| File | Model (models.py) | Server source (matterjs-server) |
| --- | --- | --- |
| `parse_onboarding_payload.value.manual21.json` | `ManualOnboardingPayloadResult` :933-944 | `WebSocketControllerHandler.ts:1366-1375` |
| `open_commissioning_window.request.python.json` | `open_commissioning_window` (client.py:437-460) | `WebSocketControllerHandler.ts:2316-2330` |
| `batched_device_commands.value.response.json` | `BatchedCommandResult` :903-909 | `ControllerCommandHandler.ts:1832-1917` |
| `icd_state.unsupported.value.json` | `IcdStateData` :293-303 | `ControllerCommandHandler.ts:2581-2594` |
| `register_icd.error.json` | `ErrorResultMessage` :216-224, code 100 | `WebSocketMessageTypes.ts:266-272` |
| `commission_with_code.error.attestation.json` | `ErrorResultMessage`, reason `attestation-rejected` | `ControllerCommandHandler.ts:305-345` |
| `get_network_topology.value.json` | `NetworkTopology` :369-380 and its nodes and connections :306-366 | `NetworkTopologyService.ts:304-430` |
| `network_topology_updated.event.json` | `EventType.NETWORK_TOPOLOGY_UPDATED` :29 | `NetworkTopologyService.ts` |
| `import_test_node.request.python.json` | `import_test_node` (client.py, `APICommand.IMPORT_TEST_NODE`) | `TestNodeCommandHandler.ts:117-185` |
| `get_all_credentials.value.json` | `get_all_credentials` (client.py:333-336) | `WebSocketControllerHandler.ts:2250-2302` |
| `discover.value.json` | `CommissionableNodeData` :256-277 | `ControllerCommandHandler.ts:2141-2191` |

`controllerapi/compat_internal_test.go` round-trips each through the type
the server encodes it with, as it does the documented examples.
