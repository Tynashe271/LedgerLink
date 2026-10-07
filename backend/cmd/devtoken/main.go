// Command devtoken mints a delegation token for local development and manual
// API testing, standing in for the Laravel gateway's signing step. It must
// never be deployed to a shared environment: anyone holding
// DELEGATION_SECRET can impersonate any user via this tool.
package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/ledgerlink/branchledger/backend/internal/auth"
)

func main() {
	userID := flag.String("user", "", "user UUID")
	companyID := flag.String("company", "", "company UUID")
	role := flag.String("role", "owner", "role (owner, general_manager, accountant, branch_manager, staff)")
	branches := flag.String("branches", "", "comma-separated branch UUIDs (empty = unrestricted)")
	ttl := flag.Duration("ttl", time.Minute, "token lifetime")
	flag.Parse()

	if *userID == "" || *companyID == "" {
		log.Fatal("usage: devtoken -user <uuid> -company <uuid> [-role owner] [-branches <uuid,uuid>]")
	}

	secretHex := os.Getenv("DELEGATION_SECRET")
	secret, err := hex.DecodeString(secretHex)
	if err != nil || len(secret) < 32 {
		log.Fatal("DELEGATION_SECRET must be set to a hex-encoded value of at least 32 bytes")
	}

	var branchScope []uuid.UUID
	if *branches != "" {
		for _, b := range splitComma(*branches) {
			branchScope = append(branchScope, uuid.MustParse(b))
		}
	}

	now := time.Now()
	v := auth.NewVerifier(secret, *ttl+time.Minute)
	token, err := v.Sign(auth.Claims{
		UserID: uuid.MustParse(*userID), CompanyID: uuid.MustParse(*companyID),
		Role: *role, BranchScope: branchScope, AccessVersion: 1,
		SessionID: "dev-session", RequestID: uuid.NewString(),
		IssuedAt: now, ExpiresAt: now.Add(*ttl),
	})
	if err != nil {
		log.Fatalf("sign: %v", err)
	}
	fmt.Println(token)
}

func splitComma(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}
