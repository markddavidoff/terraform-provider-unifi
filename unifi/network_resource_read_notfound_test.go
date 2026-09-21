package unifi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

// newNetworkFakeControllerClient spins up a minimal fake old-style controller
// (302 on /, cookie login at /api/login) whose networkconf endpoints are driven
// by the supplied handler, and returns a provider client wired to it.
func newNetworkFakeControllerClient(
	t *testing.T,
	networkconf http.HandlerFunc,
) *Client {
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
	mux.HandleFunc("/api/s/default/rest/networkconf/", networkconf)

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

// newNetworkReadRequestResponse builds a Read request whose prior state holds
// only the given id/site (everything else null), plus a matching response.
func newNetworkReadRequestResponse(
	ctx context.Context,
	t *testing.T,
	r *networkResource,
	id string,
	site string,
) (fwresource.ReadRequest, *fwresource.ReadResponse) {
	t.Helper()

	var schemaResp fwresource.SchemaResponse
	r.Schema(ctx, fwresource.SchemaRequest{}, &schemaResp)

	var identityResp fwresource.IdentitySchemaResponse
	r.IdentitySchema(ctx, fwresource.IdentitySchemaRequest{}, &identityResp)

	state := tfsdk.State{
		Raw:    tftypes.NewValue(schemaResp.Schema.Type().TerraformType(ctx), nil),
		Schema: schemaResp.Schema,
	}
	if diags := state.SetAttribute(ctx, path.Root("id"), id); diags.HasError() {
		t.Fatalf("setting state id: %v", diags)
	}
	if diags := state.SetAttribute(ctx, path.Root("site"), site); diags.HasError() {
		t.Fatalf("setting state site: %v", diags)
	}

	req := fwresource.ReadRequest{
		State: tfsdk.State{Raw: state.Raw.Copy(), Schema: schemaResp.Schema},
	}
	resp := &fwresource.ReadResponse{
		State: tfsdk.State{Raw: state.Raw.Copy(), Schema: schemaResp.Schema},
		Identity: &tfsdk.ResourceIdentity{
			Raw: tftypes.NewValue(
				identityResp.IdentitySchema.Type().TerraformType(ctx),
				nil,
			),
			Schema: identityResp.IdentitySchema,
		},
	}
	return req, resp
}

// TestNetworkResourceReadRemovesResourceOnNotFound guards #4: a network deleted
// out-of-band (the controller answers the read with 404) must be dropped from
// state so the next plan re-creates it, instead of failing the plan outright.
func TestNetworkResourceReadRemovesResourceOnNotFound(t *testing.T) {
	ctx := context.Background()
	r := &networkResource{
		client: newNetworkFakeControllerClient(
			t,
			func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "not found", http.StatusNotFound)
			},
		),
	}

	req, resp := newNetworkReadRequestResponse(ctx, t, r, "deleted-network-id", "default")
	r.Read(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Read on a 404 must not error, got diagnostics: %v", resp.Diagnostics)
	}
	if !resp.State.Raw.IsNull() {
		t.Fatalf("Read on a 404 must remove the resource from state, got: %v", resp.State.Raw)
	}
}

// TestNetworkResourceReadSurfacesRealErrors is the guardrail for the fix above:
// a genuine controller-side failure must still be reported as an error and must
// NOT silently drop the resource from state, which would destroy and re-create
// a live network.
func TestNetworkResourceReadSurfacesRealErrors(t *testing.T) {
	ctx := context.Background()
	r := &networkResource{
		client: newNetworkFakeControllerClient(
			t,
			func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"meta":{"rc":"error","msg":"api.err.Invalid"},"data":[]}`))
			},
		),
	}

	req, resp := newNetworkReadRequestResponse(ctx, t, r, "live-network-id", "default")
	r.Read(ctx, req, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("Read on a controller error must surface an error diagnostic")
	}
	if resp.State.Raw.IsNull() {
		t.Fatal("Read on a controller error must not remove the resource from state")
	}
}
