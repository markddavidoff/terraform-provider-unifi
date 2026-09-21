package unifi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

const dupExcludedNetworkID = "6ab1850fe6ad805b80800676"

// TestPortProfileExcludedNetworkConfIDsDuplicatesCollapse reproduces #1.
//
// A UniFi controller can hold the same network ID twice in a port profile's
// excluded_networkconf_ids. A Terraform set has no multiplicity, so the
// provider must collapse that to a single element. Instead it hands the raw
// list to types.SetValueFrom, which rejects it with "Duplicate Set Element" —
// turning a cosmetic controller quirk into a resource that can never be read,
// planned or destroyed again (only `destroy -refresh=false` clears it).
func TestPortProfileExcludedNetworkConfIDsDuplicatesCollapse(t *testing.T) {
	t.Run("portProfileToModel", func(t *testing.T) {
		ctx := context.Background()
		r := &portProfileResource{}

		model := portProfileResourceModel{}
		api := &unifi.PortProfile{
			ID:   "port-profile-id",
			Name: "p",
			ExcludedNetworkIDs: []string{
				dupExcludedNetworkID,
				dupExcludedNetworkID,
			},
		}

		diags := r.portProfileToModel(ctx, api, &model, "default")
		if diags.HasError() {
			t.Fatalf("portProfileToModel returned diagnostics: %v", diags)
		}

		elements := model.ExcludedNetworkConfIDs.Elements()
		if len(elements) != 1 {
			t.Fatalf(
				"excluded_networkconf_ids = %s (%d elements), want exactly 1",
				model.ExcludedNetworkConfIDs,
				len(elements),
			)
		}
		if got := elements[0]; !got.Equal(types.StringValue(dupExcludedNetworkID)) {
			t.Errorf("element = %s, want %q", got, dupExcludedNetworkID)
		}
	})

	t.Run("Read", func(t *testing.T) {
		ctx := context.Background()
		r := &portProfileResource{
			client: newPortProfileDuplicateExcludedFakeClient(t),
		}

		var schemaResp fwresource.SchemaResponse
		r.Schema(ctx, fwresource.SchemaRequest{}, &schemaResp)
		var identityResp fwresource.IdentitySchemaResponse
		r.IdentitySchema(ctx, fwresource.IdentitySchemaRequest{}, &identityResp)

		priorState := tfsdk.State{Schema: schemaResp.Schema}
		if diags := priorState.Set(ctx, newPortProfileStateModel()); diags.HasError() {
			t.Fatalf("building prior state: %v", diags)
		}

		resp := &fwresource.ReadResponse{
			State: priorState,
			Identity: &tfsdk.ResourceIdentity{
				Schema: identityResp.IdentitySchema,
				Raw: tftypes.NewValue(
					identityResp.IdentitySchema.Type().TerraformType(ctx),
					nil,
				),
			},
		}

		r.Read(ctx, fwresource.ReadRequest{State: priorState}, resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("Read returned diagnostics: %v", resp.Diagnostics)
		}

		var got portProfileResourceModel
		if diags := resp.State.Get(ctx, &got); diags.HasError() {
			t.Fatalf("reading back state: %v", diags)
		}
		if n := len(got.ExcludedNetworkConfIDs.Elements()); n != 1 {
			t.Fatalf(
				"excluded_networkconf_ids = %s (%d elements), want exactly 1",
				got.ExcludedNetworkConfIDs,
				n,
			)
		}
	})
}

// newPortProfileStateModel returns a fully-typed model usable as prior state.
func newPortProfileStateModel() portProfileResourceModel {
	return portProfileResourceModel{
		ID:                  types.StringValue("port-profile-id"),
		Site:                types.StringValue("default"),
		Name:                types.StringValue("p"),
		Forward:             types.StringValue("customize"),
		TaggedVLANMgmt:      types.StringValue("custom"),
		NativeNetworkConfID: types.StringValue("native-network-id"),
		ExcludedNetworkConfIDs: types.SetValueMust(
			types.StringType,
			[]attr.Value{types.StringValue(dupExcludedNetworkID)},
		),
		TaggedNetworkConfIDs:      types.SetNull(types.StringType),
		MulticastRouterNetworkIDs: types.SetNull(types.StringType),
		PortSecurityMacAddress:    types.SetNull(types.StringType),
		Dot1XIdleTimeout:          timetypes.NewGoDurationNull(),
		Timeouts:                  timeoutsNullValue(),
	}
}

// newPortProfileDuplicateExcludedFakeClient serves a single port profile whose
// excluded_networkconf_ids lists the same network twice, as the controller in
// #1 does after the profile is created.
func newPortProfileDuplicateExcludedFakeClient(t *testing.T) *Client {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/manage", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/api/login", func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "unifises", Value: "fake-session", Path: "/"})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"meta":{"rc":"ok"},"data":[]}`))
	})
	mux.HandleFunc(
		"/api/s/default/rest/portconf/port-profile-id",
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"meta":{"rc":"ok"},"data":[{` +
				`"_id":"port-profile-id",` +
				`"name":"p",` +
				`"forward":"customize",` +
				`"tagged_vlan_mgmt":"custom",` +
				`"native_networkconf_id":"native-network-id",` +
				`"excluded_networkconf_ids":["` + dupExcludedNetworkID + `","` +
				dupExcludedNetworkID + `"]}]}`))
		},
	)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	apiClient, err := unifi.New(context.Background(), &unifi.Config{
		BaseURL:  srv.URL,
		Username: "admin",
		Password: "admin",
	})
	if err != nil {
		t.Fatalf("creating client against fake controller: %v", err)
	}
	return &Client{ApiClient: apiClient, Site: "default"}
}
