package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/mileweb/terraform-provider-cdnpro/internal/client"
)

// Private state keys shared by resources.
const (
	privateLastModified = "last_modified"
	// privateAdopted marks a resource that entered the state through
	// terraform import. Its write-only values were never sent by this
	// configuration, so no hashes exist to compare the configuration against.
	// While the marker is set, the configured write-only values are taken to
	// match what is on the server. ModifyPlan makes sure the first plan after
	// the import carries a change, and the Update that applies it records the
	// hashes and clears the marker without calling the API, so later changes
	// are detected as usual. (Terraform persists private state only through
	// an apply, which is why a no-op plan cannot do the recording.)
	privateAdopted = "adopted"
)

// privateState is the subset of resource.ReadRequest/Response.Private etc. the
// helpers below need.
type privateGetter interface {
	GetKey(ctx context.Context, key string) ([]byte, diag.Diagnostics)
}

type privateSetter interface {
	SetKey(ctx context.Context, key string, value []byte) diag.Diagnostics
}

// getPrivate reads a string stored with setPrivate. Private state values must
// be JSON, so strings are stored JSON-encoded.
func getPrivate(ctx context.Context, p privateGetter, key string) string {
	if p == nil {
		return ""
	}
	raw, _ := p.GetKey(ctx, key)
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

func setPrivate(ctx context.Context, p privateSetter, key, value string, diags *diag.Diagnostics) {
	if p == nil {
		return
	}
	if value == "" {
		diags.Append(p.SetKey(ctx, key, nil)...)
		return
	}
	encoded, _ := json.Marshal(value)
	diags.Append(p.SetKey(ctx, key, encoded)...)
}

// markAdopted is called from ImportState so the next plan adopts the
// configured write-only values instead of treating them as changed.
func markAdopted(ctx context.Context, p privateSetter, diags *diag.Diagnostics) {
	setPrivate(ctx, p, privateAdopted, "1", diags)
}

// isAdopted reports whether the resource still awaits its first plan after
// import.
func isAdopted(ctx context.Context, p privateGetter) bool {
	return getPrivate(ctx, p, privateAdopted) != ""
}

// clearAdopted is called from Update once the hashes have been recorded.
func clearAdopted(ctx context.Context, p privateSetter, diags *diag.Diagnostics) {
	setPrivate(ctx, p, privateAdopted, "", diags)
}

// addAdoptionWarning is emitted by ModifyPlan for an imported resource. The
// plan it accompanies always carries a change (Terraform only persists
// private state through an apply), so the warning says what that apply does.
func addAdoptionWarning(diags *diag.Diagnostics, kind, attrs string) {
	diags.AddWarning(kind+" was imported: adopting the configured "+attrs,
		"The API never returns "+attrs+", so the configured value is taken to be the one on the server. "+
			"The apply that follows only records its hash so that later changes are detected; nothing is sent to the API.")
}

// addAPIError converts an error from the client into a diagnostic. API codes
// with special remediation get their own summary; everything else carries the
// API's code and message verbatim.
func addAPIError(diags *diag.Diagnostics, summary string, err error) {
	apiErr := client.AsAPIError(err)
	if apiErr == nil {
		var failed *client.TaskFailedError
		var ehFailed *client.EdgeHostnameFailedError
		switch {
		case errors.As(err, &failed), errors.As(err, &ehFailed):
			diags.AddError(summary, err.Error())
		case errors.Is(err, client.ErrPollTimeout):
			diags.AddError(summary+": timed out",
				err.Error()+". The operation may still complete on the server; run terraform plan again to pick up its result, "+
					"or raise the matching timeout in the provider's polling block.")
		default:
			diags.AddError(summary, err.Error())
		}
		return
	}

	switch {
	case client.IsPreconditionFailed(err):
		diags.AddError("Resource modified outside Terraform",
			fmt.Sprintf("%s: the CDN Pro API reported %s (HTTP %d): %s. The resource changed since Terraform last read it. "+
				"Run terraform plan again to refresh state and review the out-of-band change before applying.",
				summary, apiErr.Code, apiErr.StatusCode, apiErr.Message))
	case apiErr.Code == client.CodeTooManyVersions:
		diags.AddError("Version limit reached",
			fmt.Sprintf("%s: %s. The limit counts every version that ever existed, so deleting old versions does not help. "+
				"Create a new property or certificate resource and point your deployments at it instead.", summary, apiErr.Error()))
	case apiErr.Code == client.CodeInvalidCertVersion:
		diags.AddError("Certificate key type cannot change in place",
			fmt.Sprintf("%s: %s. A new version must use the same key algorithm (RSA or EC) as the existing versions. "+
				"To switch algorithms, create a new cdnpro_certificate resource and move deployments to it.", summary, apiErr.Error()))
	case apiErr.Code == client.CodeDuplicateDeployment:
		diags.AddError("A deployment to this target is already in flight",
			fmt.Sprintf("%s: %s. Re-run terraform apply once the in-flight deployment completes, or order the two resources "+
				"with depends_on so they never target the same environment concurrently.", summary, apiErr.Error()))
	case apiErr.Code == client.CodeFrozenPropertyVersion:
		diags.AddError("Attempted to edit a frozen property version",
			fmt.Sprintf("%s: %s. This should not happen because the provider checks the frozen flag first; "+
				"please report it as a provider bug.", summary, apiErr.Error()))
	default:
		diags.AddError(summary, apiErr.Error())
	}
}

// sha256Hex returns the hex SHA-256 of s, used to track write-only attributes
// without storing them.
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// Conversions between framework values and the pointer-based client structs.

func strPtr(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	s := v.ValueString()
	return &s
}

func strPtrNonEmpty(v types.String) *string {
	p := strPtr(v)
	if p != nil && *p == "" {
		return nil
	}
	return p
}

func boolPtr(v types.Bool) *bool {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	b := v.ValueBool()
	return &b
}

func int64Ptr(v types.Int64) *int64 {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	n := v.ValueInt64()
	return &n
}

func strFromPtr(p *string) types.String {
	if p == nil {
		return types.StringNull()
	}
	return types.StringValue(*p)
}

// strOrNull maps an API string to a Terraform string, treating "" as null.
func strOrNull(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

func boolFromPtr(p *bool) types.Bool {
	if p == nil {
		return types.BoolNull()
	}
	return types.BoolValue(*p)
}

func int64FromPtr(p *int64) types.Int64 {
	if p == nil {
		return types.Int64Null()
	}
	return types.Int64Value(*p)
}

func stringList(ctx context.Context, values []string, diags *diag.Diagnostics) types.List {
	if values == nil {
		return types.ListNull(types.StringType)
	}
	l, d := types.ListValueFrom(ctx, types.StringType, values)
	diags.Append(d...)
	return l
}

func stringListOrEmpty(ctx context.Context, values []string, diags *diag.Diagnostics) types.List {
	if values == nil {
		values = []string{}
	}
	return stringList(ctx, values, diags)
}

func stringSet(ctx context.Context, values []string, diags *diag.Diagnostics) types.Set {
	if values == nil {
		return types.SetNull(types.StringType)
	}
	s, d := types.SetValueFrom(ctx, types.StringType, values)
	diags.Append(d...)
	return s
}

func int64List(ctx context.Context, values []int64, diags *diag.Diagnostics) types.List {
	if values == nil {
		return types.ListNull(types.Int64Type)
	}
	l, d := types.ListValueFrom(ctx, types.Int64Type, values)
	diags.Append(d...)
	return l
}

func listToStrings(ctx context.Context, l types.List, diags *diag.Diagnostics) []string {
	if l.IsNull() || l.IsUnknown() {
		return nil
	}
	var out []string
	diags.Append(l.ElementsAs(ctx, &out, false)...)
	return out
}

func setToStrings(ctx context.Context, s types.Set, diags *diag.Diagnostics) []string {
	if s.IsNull() || s.IsUnknown() {
		return nil
	}
	var out []string
	diags.Append(s.ElementsAs(ctx, &out, false)...)
	return out
}

func listToInt64s(ctx context.Context, l types.List, diags *diag.Diagnostics) []int64 {
	if l.IsNull() || l.IsUnknown() {
		return nil
	}
	var out []int64
	diags.Append(l.ElementsAs(ctx, &out, false)...)
	return out
}

// keepCase returns prior when it equals current ignoring case, so that a value
// the API canonicalises ("Off" vs "off") does not show up as drift.
func keepCase(prior types.String, current string) types.String {
	if current == "" {
		return types.StringNull()
	}
	if !prior.IsNull() && !prior.IsUnknown() && strings.EqualFold(prior.ValueString(), current) {
		return prior
	}
	return types.StringValue(current)
}
