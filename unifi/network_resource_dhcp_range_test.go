package unifi

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/cidrtypes"
	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

// Test_networkResource_modelToNetwork_omitsEmptyDHCPRange covers #2.
//
// `dhcp_server.start`/`.stop` are Optional+Computed with no default, so at
// create with `dhcp_server = { enabled = false }` they arrive in the plan as
// *unknown*. ValueStringPointer() returns nil only for a *null* value — for an
// unknown one it returns a pointer to "". go-unifi's Network.MarshalJSON
// substitutes the subnet-derived DHCP range via valueOrDefault(n.DHCPDStart,
// defaultStart), which only fires when the pointer is nil, so the
// pointer-to-"" survives and the create payload carries `"dhcpd_start": ""` /
// `"dhcpd_stop": ""` next to the schema default `"setting_preference":
// "auto"`. Network 10.6.101 answers that exact combination with an unhandled
// HTTP 500 ("auto" alone is 200; an empty range alone is a clean 400).
//
// The contract this test pins: modelToNetwork must hand go-unifi a nil pointer
// when there is no configured range, so an empty dhcpd_start/dhcpd_stop never
// reaches the controller.
func Test_networkResource_modelToNetwork_omitsEmptyDHCPRange(t *testing.T) {
	r := &networkResource{}
	ctx := context.Background()

	dhcpServerObj := func(start, stop types.String) types.Object {
		return types.ObjectValueMust(dhcpServerModel{}.AttributeTypes(), map[string]attr.Value{
			"boot":                types.ObjectNull(dhcpBootModel{}.AttributeTypes()),
			"enabled":             types.BoolValue(false),
			"start":               start,
			"stop":                stop,
			"gateway_enabled":     types.BoolValue(false),
			"conflict_checking":   types.BoolValue(true),
			"ntp_enabled":         types.BoolValue(false),
			"ntp_servers":         types.ListNull(types.StringType),
			"time_offset_enabled": types.BoolValue(false),
			"dns_enabled":         types.BoolValue(false),
			"leasetime":           timetypes.NewGoDurationNull(),
			"wins":                types.ObjectNull(winsModel{}.AttributeTypes()),
			"wpad_url":            types.StringNull(),
			"tftp_server":         types.StringNull(),
			"unifi_controller":    types.StringNull(),
			"dns_servers":         types.ListNull(types.StringType),
		})
	}

	baseModel := func(dhcpServer types.Object) *networkResourceModel {
		return &networkResourceModel{
			Name:              types.StringValue("poc"),
			Subnet:            cidrtypes.NewIPv4PrefixValue("192.168.99.1/24"),
			ThirdPartyGateway: types.BoolValue(false),
			Purpose:           types.StringValue(unifi.PurposeCorporate),
			// Schema default — this is what makes an empty range fatal.
			SettingPreference: types.StringValue("auto"),
			NatOutboundIPAddresses: types.ListNull(
				types.ObjectType{AttrTypes: natOutboundIPAddresses()},
			),
			IPAliases:    types.ListNull(types.StringType),
			IPv6Aliases:  types.ListNull(types.StringType),
			DhcpServer:   dhcpServer,
			DhcpRelay:    types.ObjectNull(dhcpRelayModel{}.AttributeTypes()),
			DhcpV6Server: types.ObjectNull(dhcpV6ServerModel{}.AttributeTypes()),
			DhcpGuarding: types.ObjectNull(dhcpGuardingModel{}.AttributeTypes()),
		}
	}

	tests := []struct {
		name string
		// start/stop as they appear in the plan handed to modelToNetwork.
		start, stop types.String
		// wantSent: the range must survive to the payload verbatim.
		wantSent  bool
		wantStart string
		wantStop  string
	}{
		{
			name:  "unknown range (computed, not configured) is not sent as empty",
			start: types.StringUnknown(),
			stop:  types.StringUnknown(),
		},
		{
			name:  "null range is not sent as empty",
			start: types.StringNull(),
			stop:  types.StringNull(),
		},
		{
			name:  "explicit empty-string range is not sent as empty",
			start: types.StringValue(""),
			stop:  types.StringValue(""),
		},
		{
			name:      "configured range is still sent verbatim",
			start:     types.StringValue("192.168.99.20"),
			stop:      types.StringValue("192.168.99.200"),
			wantSent:  true,
			wantStart: "192.168.99.20",
			wantStop:  "192.168.99.200",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			network, diags := r.modelToNetwork(ctx, baseModel(dhcpServerObj(tt.start, tt.stop)))
			if diags.HasError() {
				t.Fatalf("modelToNetwork: %v", diags)
			}

			// go-unifi marshals the *unifi.Network straight into the POST body
			// (createNetwork -> c.do), and Network.MarshalJSON is where the
			// subnet-derived default range is applied — so this is
			// exactly what the controller receives.
			body, err := json.Marshal(network)
			if err != nil {
				t.Fatalf("json.Marshal(network): %v", err)
			}
			var payload map[string]json.RawMessage
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("json.Unmarshal payload: %v", err)
			}

			// Pin the combination under test: without setting_preference
			// "auto" this payload would not reproduce the 500.
			if got, ok := payload["setting_preference"]; !ok || string(got) != `"auto"` {
				t.Fatalf(
					"setting_preference = %s (present=%v), want \"auto\" — test no longer pins the failing combination",
					got,
					ok,
				)
			}

			// An empty string must never reach the wire for either key: with
			// setting_preference "auto" that is an unhandled 500 on Network
			// 10.6.101.
			for _, key := range []string{"dhcpd_start", "dhcpd_stop"} {
				if raw, ok := payload[key]; ok && string(raw) == `""` {
					t.Errorf(
						"payload %s is an empty string; want it omitted or a real address (empty range + setting_preference=auto is an unhandled 500 on Network 10.6.101)",
						key,
					)
				}
			}

			if !tt.wantSent {
				// modelToNetwork must hand go-unifi nil so its MarshalJSON can
				// substitute the subnet-derived default range.
				if network.DHCPDStart != nil {
					t.Errorf(
						"DHCPDStart = %q, want nil so go-unifi applies its subnet-derived default",
						*network.DHCPDStart,
					)
				}
				if network.DHCPDStop != nil {
					t.Errorf(
						"DHCPDStop = %q, want nil so go-unifi applies its subnet-derived default",
						*network.DHCPDStop,
					)
				}
				return
			}

			if network.DHCPDStart == nil || *network.DHCPDStart != tt.wantStart {
				t.Errorf("DHCPDStart = %v, want %q", network.DHCPDStart, tt.wantStart)
			}
			if network.DHCPDStop == nil || *network.DHCPDStop != tt.wantStop {
				t.Errorf("DHCPDStop = %v, want %q", network.DHCPDStop, tt.wantStop)
			}
			if raw, ok := payload["dhcpd_start"]; !ok || string(raw) != `"`+tt.wantStart+`"` {
				t.Errorf("payload dhcpd_start = %s (present=%v), want %q", raw, ok, tt.wantStart)
			}
			if raw, ok := payload["dhcpd_stop"]; !ok || string(raw) != `"`+tt.wantStop+`"` {
				t.Errorf("payload dhcpd_stop = %s (present=%v), want %q", raw, ok, tt.wantStop)
			}
		})
	}
}
