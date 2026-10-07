-- Section 4.6's "needsDelegation" grants (reverse postings, approve
-- spending, lock periods, manage users, export data, change chart of
-- accounts, and a general manager's company-wide view) were enforced in
-- backend/internal/tenancy/permissions.go but had nowhere to actually be
-- recorded per membership. Laravel reads this column to decide what to put
-- in the delegation token it signs for Go (DelegationTokenSigner::sign).
ALTER TABLE memberships ADD COLUMN delegations text[] NOT NULL DEFAULT '{}';
