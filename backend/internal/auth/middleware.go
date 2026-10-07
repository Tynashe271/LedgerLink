package auth

import (
	"net/http"

	"github.com/ledgerlink/branchledger/backend/internal/tenancy"
)

// DelegationHeader is the only header Go trusts for identity. Per the
// architecture doc's "Reject client-provided identity headers," any other
// identity-shaped header (X-User-Id, X-Company-Id, etc.) arriving from the
// browser or an untrusted hop must be stripped by the reverse proxy / gateway
// before reaching Go, and this middleware never reads them.
const DelegationHeader = "X-BranchLedger-Delegation"

// RequireDelegation verifies the signed delegation token on every request and,
// on success, attaches a tenancy.Scope to the request context. On failure it
// writes 401 and stops the chain — there is no fallback identity.
func RequireDelegation(verifier *Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := r.Header.Get(DelegationHeader)
			claims, err := verifier.Verify(token)
			if err != nil {
				http.Error(w, `{"error_code":"unauthenticated"}`, http.StatusUnauthorized)
				return
			}

			delegations := make(tenancy.Delegations, len(claims.Delegations))
			for _, a := range claims.Delegations {
				delegations[tenancy.Action(a)] = true
			}

			scope := tenancy.Scope{
				CompanyID:     claims.CompanyID,
				UserID:        claims.UserID,
				Role:          tenancy.Role(claims.Role),
				BranchScope:   claims.BranchScope,
				AccessVersion: claims.AccessVersion,
				RequestID:     claims.RequestID,
				Delegations:   delegations,
			}

			ctx := tenancy.WithScope(r.Context(), scope)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
