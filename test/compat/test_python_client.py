# SPDX-License-Identifier: Apache-2.0
# Copyright (C) 2026 SukramJ.
"""The python-client compatibility job (brief S3 §4).

matterjs-server's python client at the version Home Assistant's Matter
integration pins (matter-python-client==1.4.0, core 2026.10.3) against a
running fabric-server: the calls the integration makes after connect, and
the commands of schema 14 the 1.4.0 client has no method for, sent through
its generic send_command. FABRIC_SERVER_URL names the server; with
FABRIC_SERVER_BRIDGE_DISCRIMINATOR and FABRIC_SERVER_BRIDGE_PASSCODE a
commissionable device is on the network and is commissioned first.
"""

from __future__ import annotations

import asyncio
import os

import aiohttp
import pytest
import pytest_asyncio
from matter_server.client import MatterClient
from matter_server.common.errors import (
    InvalidArguments,
    InvalidCommand,
    MatterError,
    NodeNotExists,
)
from matter_server.common.models import EventType

URL = os.environ.get("FABRIC_SERVER_URL", "ws://127.0.0.1:5580/ws")
BRIDGE_DISCRIMINATOR = os.environ.get("FABRIC_SERVER_BRIDGE_DISCRIMINATOR")
BRIDGE_PASSCODE = os.environ.get("FABRIC_SERVER_BRIDGE_PASSCODE")


@pytest_asyncio.fixture
async def client():
    """A connected, listening client, as the integration's adapter runs it."""
    async with aiohttp.ClientSession() as session:
        c = MatterClient(URL, session)
        await c.connect()
        ready = asyncio.Event()
        task = asyncio.create_task(c.start_listening(ready))
        await asyncio.wait_for(ready.wait(), 30)
        try:
            yield c
        finally:
            await c.disconnect()
            task.cancel()


@pytest.mark.asyncio
async def test_server_info(client):
    """Schema 14, minimum 11: the numbers the client and the integration check."""
    info = client.server_info
    assert info is not None
    assert info.schema_version == 14
    assert info.min_supported_schema_version == 11
    assert info.sdk_version.startswith("go-fabric-server/")
    assert info.fabric_id > 0
    assert info.compressed_fabric_id > 0


@pytest.mark.asyncio
async def test_nodes_and_vendor_names(client):
    """get_nodes after start_listening; get_vendor_names answers a map."""
    nodes = client.get_nodes()
    raw = await client.send_command("get_nodes")
    assert len(raw) == len(nodes)
    vendors = await client.send_command("get_vendor_names", filter_vendors=[65521])
    assert isinstance(vendors, dict)


@pytest.mark.asyncio
async def test_fabric_label(client):
    """set_default_fabric_label as the integration calls it, and back."""
    await client.set_default_fabric_label("HomeAssistant")
    assert await client.get_fabric_label() == "HomeAssistant"
    await client.set_default_fabric_label(None)
    assert await client.get_fabric_label() == "HomeAssistant"


@pytest.mark.asyncio
async def test_discover(client):
    """discover answers a list (empty without a commissionable device)."""
    found = await client.discover_commissionable_nodes()
    assert isinstance(found, list)
    if BRIDGE_DISCRIMINATOR is None:
        return
    for node in found:
        assert node.long_discriminator is not None


@pytest.mark.asyncio
async def test_parse_onboarding_payload(client):
    """The documented examples (docs/websockets_api.md), sent generically."""
    qr = await client.send_command("parse_onboarding_payload", require_schema=14, code="MT:Y.K90AFN00KA0648G00")
    assert qr == {
        "type": "qr",
        "payloads": [
            {
                "version": 0,
                "vendor_id": 65521,
                "product_id": 32768,
                "flow_type": 0,
                "discovery_capabilities": 4,
                "discriminator": 3840,
                "setup_pin_code": 20202021,
            }
        ],
    }
    manual = await client.send_command("parse_onboarding_payload", require_schema=14, code="34970112332")
    assert manual == {"type": "manual", "short_discriminator": 15, "setup_pin_code": 20202021}
    with pytest.raises(InvalidArguments):
        await client.send_command("parse_onboarding_payload", require_schema=14, code="34970112333")


@pytest.mark.asyncio
async def test_open_commissioning_window_on_a_missing_node(client):
    """The error shape the integration's open_commissioning_window meets."""
    with pytest.raises(NodeNotExists):
        await client.open_commissioning_window(4242)


@pytest.mark.asyncio
async def test_not_implemented(client):
    """A command the server does not answer is InvalidCommand (9)."""
    with pytest.raises(InvalidCommand):
        await client.send_command("update_node", node_id=1, software_version=2)


@pytest.mark.asyncio
async def test_event_subscription(client):
    """subscribe_events: a server_info_updated reaches the callback."""
    got = asyncio.get_running_loop().create_future()

    def on_event(event, data):
        if event == EventType.SERVER_INFO_UPDATED and not got.done():
            got.set_result(data)

    unsubscribe = client.subscribe_events(on_event)
    try:
        await client.set_wifi_credentials("compat-ssid", "compat-pass")
        data = await asyncio.wait_for(got, 10)
        assert data is not None
    finally:
        unsubscribe()
        await client.remove_wifi_credentials()


@pytest.mark.asyncio
async def test_commission_a_device(client):
    """commission_on_network as the integration calls it, then the node."""
    if BRIDGE_DISCRIMINATOR is None or BRIDGE_PASSCODE is None:
        pytest.skip("no commissionable device in this run")
    added = asyncio.get_running_loop().create_future()

    def on_added(event, data):
        if not added.done():
            added.set_result(data)

    unsubscribe = client.subscribe_events(on_added, EventType.NODE_ADDED)
    try:
        # The 1.4.0 client's commission_on_network takes no filter; the
        # filter by long discriminator keeps the job to its own device.
        data = await client.send_command(
            "commission_on_network",
            setup_pin_code=int(BRIDGE_PASSCODE),
            filter_type=2,
            filter=int(BRIDGE_DISCRIMINATOR),
        )
        await asyncio.wait_for(added, 30)
    finally:
        unsubscribe()
    node_id = data["node_id"]
    assert node_id > 0
    assert data["attributes"]
    node = client.get_node(node_id)
    assert node.node_id == node_id
    assert await client.get_node_ip_addresses(node_id)
    assert await client.ping_node(node_id)
    await client.interview_node(node_id)
    fabrics = await client.get_matter_fabrics(node_id)
    assert len(fabrics) >= 1
    topology = await client.get_network_topology()
    assert any(n.node_id == node_id for n in topology.nodes)
    icd = await client.get_icd_state(node_id)
    assert icd.supported is False
    window = await client.open_commissioning_window(node_id)
    assert window.setup_pin_code > 0
    assert window.setup_qr_code.startswith("MT:")
    await client.remove_node(node_id)
    with pytest.raises(MatterError):
        await client.send_command("get_node", node_id=node_id)
