// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/node"

	"github.com/SukramJ/go-fabric-server/controllerapi"
	"github.com/SukramJ/go-fabric-server/protocol"
)

// icdNodeConfig is a LIT ICD node of the program's own composition
// (fabrictest.Site.AddNode), with matter.js IcdManagementServerTest's LIT
// timings and room for two clients.
func icdNodeConfig() node.Config {
	return node.Config{
		Network: node.Network{Listen: "[::]:5540"},
		Commissioning: node.Commissioning{
			Passcode: 34567890, Discriminator: 1234, PBKDFIterations: 1000, Salt: []byte("SPAKE2P Key Salt icd node"),
		},
		ProductDescription: node.ProductDescription{VendorID: 0xFFF1, ProductID: 0x8003},
		BasicInformation: node.BasicInformation{
			VendorName: "test", ProductName: "icd", NodeLabel: "icd", SerialNumber: "ICD-1",
			HardwareVersion: 1, HardwareVersionString: "1", SoftwareVersion: 1, SoftwareVersionString: "1",
		},
		ICD: &node.ICDConfig{
			Mode: node.ICDModeLIT, ActiveModeThreshold: 5 * time.Second, IdleModeDuration: time.Hour,
			ActiveModeDuration: time.Second, MaximumCheckInBackoff: time.Hour, ClientsSupportedPerFabric: 2,
			UserActiveModeTrigger: &node.ICDUserActiveModeTrigger{Hint: 1},
		},
	}
}

type icdState struct {
	Supported           bool    `json:"supported"`
	LITSupported        bool    `json:"lit_supported"`
	Registered          bool    `json:"registered"`
	OperatingMode       *string `json:"operating_mode"`
	Awake               *bool   `json:"awake"`
	Available           *bool   `json:"available"`
	NextExpectedCheckIn *int64  `json:"next_expected_checkin"`
}

func TestICDCommands(t *testing.T) {
	e := newEnv(t, envOptions{})
	light := e.commission()
	icdDev := e.site.AddNode(icdNodeConfig())
	n, err := e.client.CommissionOnNetwork(e.ctx, icdDev.Passcode(), 2, uint64(icdDev.Discriminator()), "")
	if err != nil {
		t.Fatalf("commission the ICD node: %v", err)
	}
	ctx := e.ctx
	// A node without ICD Management: not supported, not an error
	// (ControllerCommandHandler.ts:2575-2588).
	var st icdState
	if err := e.client.Call(ctx, "get_icd_state", map[string]any{"node_id": light.NodeID}, &st); err != nil || st.Supported || st.OperatingMode != nil {
		t.Fatalf("the light: %+v %v", st, err)
	}
	raw, err := e.client.CallRaw(ctx, "get_icd_state", map[string]any{"node_id": light.NodeID})
	if err != nil || !sameJSON(t, raw, []byte(`{"supported":false,"lit_supported":false,"registered":false,"operating_mode":null,"awake":null,"available":null,"next_expected_checkin":null}`)) {
		t.Fatalf("every key present: %s %v", raw, err)
	}
	waitFor(t, func() bool {
		st = icdState{}
		return e.client.Call(ctx, "get_icd_state", map[string]any{"node_id": n.NodeID}, &st) == nil && st.Supported
	})
	// A LIT node without a registered client may operate as SIT (matter.js
	// IcdMode.ts); the mode is one of the two.
	if st.OperatingMode == nil || (*st.OperatingMode != "LIT" && *st.OperatingMode != "SIT") || !st.LITSupported || st.Awake == nil || st.Available == nil {
		t.Fatalf("the ICD node: %+v", st)
	}
	// Registration needs the server's subscription active (go-fabric's
	// ICDClient refuses a stale operating mode): wait for the node.
	waitFor(t, func() bool {
		d, err := e.client.GetNode(ctx, n.NodeID)
		return err == nil && d.Available
	})
	// Registration (the server's subscription may have registered a LIT
	// peer already, matter.js's auto-registration): unregister, register,
	// resync, unregister with force.
	if err := e.client.Call(ctx, "unregister_icd", map[string]any{"node_id": n.NodeID}, &st); err != nil && st.Registered {
		t.Fatalf("unregister_icd: %+v %v", st, err)
	}
	if err := e.client.Call(ctx, "register_icd", map[string]any{"node_id": n.NodeID, "allow_multi_admin": false, "ignored_vendors": []int{4874}}, &st); err != nil || !st.Registered {
		t.Fatalf("register_icd: %+v %v", st, err)
	}
	e.waitEvent(e.client, "node_updated", nil)
	if err := e.client.Call(ctx, "resync_icd", map[string]any{"node_id": n.NodeID}, nil); err != nil {
		t.Fatalf("resync_icd: %v", err)
	}
	if err := e.client.Call(ctx, "unregister_icd", map[string]any{"node_id": n.NodeID, "force": true}, &st); err != nil || st.Registered {
		t.Fatalf("unregister_icd force: %+v %v", st, err)
	}
	for _, cmd := range []string{"get_icd_state", "register_icd", "resync_icd", "unregister_icd"} {
		wantCode(t, e.client.Call(ctx, cmd, map[string]any{"node_id": uint64(0xFFFF_FFFE_0000_0000)}, nil), protocol.InvalidArguments, "not supported for test nodes")
		wantCode(t, e.client.Call(ctx, cmd, map[string]any{"node_id": 4242}, nil), protocol.NodeNotExists, "")
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in 30 s")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// testDump is a Home Assistant diagnostics dump of one node
// (TestNodeCommandHandler.ts:117-140: data.node).
const testDump = `{"data":{"node":{"node_id":5,"date_commissioned":"2026-01-01T00:00:00.000000","last_interview":"2026-01-01T00:00:00.000000","interview_version":6,"available":true,"is_bridge":false,` +
	`"attributes":{"0/40/1":"Vendor","1/6/0":true,"1/8/0":128,"0/49/65532":4},"attribute_subscriptions":[],"endpoint_parents":null}}}`

func TestImportTestNode(t *testing.T) {
	e := newEnv(t, envOptions{})
	ctx := e.ctx
	if err := e.client.Call(ctx, "import_test_node", map[string]any{"dump": testDump}, nil); err != nil {
		t.Fatal(err)
	}
	var added struct {
		NodeID     uint64                     `json:"node_id"`
		Attributes map[string]json.RawMessage `json:"attributes"`
	}
	decode(t, e.waitEvent(e.client, "node_added", nil), &added)
	const first = uint64(0xFFFF_FFFE_0000_0000)
	if added.NodeID != first || string(added.Attributes["1/6/0"]) != "true" {
		t.Fatalf("node_added %+v", added)
	}
	id := added.NodeID
	nodes, err := e.client.GetNodes(ctx, false)
	if err != nil || len(nodes) != 1 || nodes[0].NodeID != id {
		t.Fatalf("get_nodes %+v %v", nodes, err)
	}
	var attrs map[string]json.RawMessage
	if err := e.client.Call(ctx, "read_attribute", map[string]any{"node_id": id, "attribute_path": "1/*/*"}, &attrs); err != nil || len(attrs) != 2 {
		t.Fatalf("read_attribute wildcard %v %v", attrs, err)
	}
	var w []map[string]any
	if err := e.client.Call(ctx, "write_attribute", map[string]any{"node_id": id, "attribute_path": "1/6/0", "value": false}, &w); err != nil || len(w) != 1 || fmt.Sprint(w[0]["Status"]) != "0" {
		t.Fatalf("write_attribute %v %v", w, err)
	}
	wantCode(t, e.client.Call(ctx, "write_attribute", map[string]any{"node_id": id, "attribute_path": "*/6/0", "value": false}, nil), protocol.InvalidArguments, "needs a specific endpoint")
	if raw, err := e.client.CallRaw(ctx, "device_command", map[string]any{"node_id": id, "endpoint_id": 1, "cluster_id": 6, "command_name": "On"}); err != nil || string(raw) != "null" {
		t.Fatalf("device_command %s %v", raw, err)
	}
	var batch []map[string]any
	if err := e.client.Call(ctx, "batched_device_commands", map[string]any{"node_id": id, "commands": []any{map[string]any{}, map[string]any{}}}, &batch); err != nil || len(batch) != 2 {
		t.Fatalf("batched %v %v", batch, err)
	}
	if ips, err := e.client.CallRaw(ctx, "get_node_ip_addresses", map[string]any{"node_id": id}); err != nil || string(ips) != `["0.0.0.0","0000:1111:2222:3333:4444"]` {
		t.Fatalf("ip addresses %s %v", ips, err)
	}
	if err := e.client.Call(ctx, "interview_node", map[string]any{"node_id": id}, nil); err != nil {
		t.Fatal(err)
	}
	e.waitEvent(e.client, "node_updated", nil)
	wantCode(t, e.client.Call(ctx, "read", map[string]any{"node_id": id, "attribute_paths": []string{"1/6/0"}}, nil), protocol.InvalidArguments, "not supported for test nodes")
	wantCode(t, e.client.Call(ctx, "set_acl_entry", map[string]any{"node_id": id, "entry": []any{}}, nil), protocol.NodeNotExists, "")
	wantCode(t, e.client.Call(ctx, "get_node", map[string]any{"node_id": id + 1}, nil), protocol.NodeNotExists, "")
	// The second import takes the next id.
	if err := e.client.Call(ctx, "import_test_node", map[string]any{"dump": `{"data":{"server":{"nodes":{"7":{"node_id":7,"attributes":{}}}}}}`}, nil); err != nil {
		t.Fatal(err)
	}
	if n, err := e.client.GetNode(ctx, id+1); err != nil || n.NodeID != id+1 {
		t.Fatalf("second import %+v %v", n, err)
	}
	if err := e.client.RemoveNode(ctx, id); err != nil {
		t.Fatal(err)
	}
	e.waitEvent(e.client, "node_removed", func(raw json.RawMessage) bool { return string(raw) == strconv.FormatUint(id, 10) })
	for _, bad := range []string{`{}`, `{"data":{}}`, `not json`} {
		wantCode(t, e.client.Call(ctx, "import_test_node", map[string]any{"dump": bad}, nil), protocol.InvalidArguments, "Invalid dump format")
	}
	wantCode(t, e.client.Call(ctx, "import_test_node", nil, nil), protocol.InvalidArguments, "dump")
}

func TestNetworkTopologyCommand(t *testing.T) {
	e := newEnv(t, envOptions{})
	light := e.commission()
	var topo struct {
		CollectedAt int64            `json:"collected_at"`
		Nodes       []map[string]any `json:"nodes"`
		Connections []map[string]any `json:"connections"`
	}
	if err := e.client.Call(e.ctx, "get_network_topology", map[string]any{"refresh": true}, &topo); err != nil {
		t.Fatal(err)
	}
	if topo.CollectedAt == 0 || len(topo.Nodes) != 1 || topo.Nodes[0]["id"] != strconv.FormatUint(light.NodeID, 10) || topo.Nodes[0]["kind"] != "matter" {
		t.Fatalf("topology %+v", topo)
	}
	// The connection is opted in: an imported Thread node changes the
	// topology and network_topology_updated follows.
	dump := `{"data":{"node":{"node_id":1,"available":true,"attributes":{"0/49/65532":2,"0/53/1":5,"0/53/64":1024}}}}`
	if err := e.client.Call(e.ctx, "import_test_node", map[string]any{"dump": dump}, nil); err != nil {
		t.Fatal(err)
	}
	raw := e.waitEvent(e.client, "network_topology_updated", func(raw json.RawMessage) bool { return strings.Contains(string(raw), `"network_type":"thread"`) })
	if !strings.Contains(string(raw), `"role":"router"`) || !strings.Contains(string(raw), `"rloc16":1024`) {
		t.Fatalf("network_topology_updated %s", raw)
	}
}

func TestFabricLabelPushed(t *testing.T) {
	e := newEnv(t, envOptions{})
	n := e.commission()
	if err := e.client.SetDefaultFabricLabel(e.ctx, "  Home Assistant Kitchen Controller Long Label  "); err != nil {
		t.Fatal(err)
	}
	want := "Home Assistant Kitchen Controlle" // trimmed, 32 characters
	if l, _ := e.client.GetFabricLabel(e.ctx); l != want {
		t.Fatalf("label %q", l)
	}
	waitFor(t, func() bool {
		f, err := e.client.GetMatterFabrics(e.ctx, n.NodeID)
		return err == nil && len(f) == 1 && f[0]["fabric_label"] == want
	})
}

func TestThreadDataset(t *testing.T) {
	e := newEnv(t, envOptions{})
	// chip's OpenThreadDemo dataset: network name and extended PAN id.
	const ds = "0e080000000000010000000300000f35060004001fffe0020811111111222222220708fd000000000000000510001122334455667788" +
		"99aabbccddeeff030e4f70656e54687265616444656d6f010212340410445f2b5ca6f2a93a55ce570a70efeecb0c0402a0f7f8"
	if err := e.client.SetThreadDataset(e.ctx, ds); err != nil {
		t.Fatal(err)
	}
	all, err := e.client.GetAllCredentials(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(all["thread"]); !strings.Contains(got, "networkName:OpenThreadDemo") || !strings.Contains(got, "extPanId:1111111122222222") || strings.Contains(got, "445f2b5c") {
		t.Fatalf("thread summaries %s", got)
	}
	// Hex, but not MeshCoP TLVs.
	wantCode(t, e.client.SetThreadDataset(e.ctx, "0e"), protocol.InvalidArguments, "Invalid Thread operational dataset")
	wantCode(t, e.client.Call(e.ctx, "commission_with_code", map[string]any{"code": "34970112332", "wifi_credentials_id": "nope"}, nil), protocol.InvalidArguments, "Unknown wifi_credentials_id: nope")
}

// logBuffer collects every log line of the server.
type logBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// TestSecretsNotLogged: passcodes, Wi-Fi credentials and Thread datasets
// never reach the log (brief S3, hard rule 4), at debug level.
func TestSecretsNotLogged(t *testing.T) {
	var logs logBuffer
	log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	// The controller logs to the same handler, as in the daemon.
	e := newEnv(t, envOptions{server: controllerapi.Options{Logger: log}, configure: func(cfg *controller.Config) { cfg.Logger = log }})
	n, err := e.client.CommissionOnNetwork(e.ctx, e.dev.Passcode(), 2, uint64(e.dev.Discriminator()), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.client.SetWifiCredentials(e.ctx, "home", "wifi-pass-9137"); err != nil {
		t.Fatal(err)
	}
	const dataset = "0e080000000000010000000300000f"
	if err := e.client.SetThreadDataset(e.ctx, dataset); err != nil {
		t.Fatal(err)
	}
	w, err := e.client.OpenCommissioningWindow(e.ctx, n.NodeID, 300)
	if err != nil {
		t.Fatal(err)
	}
	// Failed calls carry their arguments in no log line either.
	_ = e.client.Call(e.ctx, "set_wifi_credentials", map[string]any{"ssid": 1, "credentials": "wifi-pass-9137"}, nil)
	_ = e.client.Call(e.ctx, "commission_on_network", map[string]any{"setup_pin_code": 99999999, "filter_type": 9, "filter": 1}, nil)
	out := logs.String()
	for _, secret := range []string{
		"wifi-pass-9137", dataset, "99999999", strconv.FormatUint(uint64(e.dev.Passcode()), 10),
		fmt.Sprint(w["setup_pin_code"]), fmt.Sprint(w["setup_manual_code"]), fmt.Sprint(w["setup_qr_code"]),
	} {
		if strings.Contains(out, secret) {
			t.Errorf("the log holds a secret %q", secret)
		}
	}
	if !strings.Contains(out, "fabricserver.ws.request") {
		t.Fatal("no request was logged: the check saw nothing")
	}
}
