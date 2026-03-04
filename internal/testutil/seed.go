package testutil

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// hashRaw returns the SHA-256 hex digest of raw — identical to the production
// auth middleware's hashToken function.
func hashRaw(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// randHex returns n random bytes encoded as a lowercase hex string.
func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b) //nolint:errcheck
	return hex.EncodeToString(b)
}

// CreateUser inserts a user with a bcrypt-hashed password and returns the user UUID string.
func CreateUser(t *testing.T, pool *pgxpool.Pool, email, password, displayName string) string {
	t.Helper()
	return createUser(t, pool, email, password, displayName, false)
}

// CreateAdminUser inserts a user with is_admin=true and returns the user UUID string.
func CreateAdminUser(t *testing.T, pool *pgxpool.Pool, email, password, displayName string) string {
	t.Helper()
	return createUser(t, pool, email, password, displayName, true)
}

func createUser(t *testing.T, pool *pgxpool.Pool, email, password, displayName string, isAdmin bool) string {
	t.Helper()
	ctx := context.Background()

	var userID string
	err := pool.QueryRow(ctx,
		`INSERT INTO users (display_name, email, is_admin) VALUES ($1, $2, $3) RETURNING id`,
		displayName, email, isAdmin,
	).Scan(&userID)
	if err != nil {
		t.Fatalf("testutil: CreateUser: %v", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("testutil: bcrypt: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO user_credentials (user_id, password_hash) VALUES ($1, $2)`,
		userID, string(hash),
	); err != nil {
		t.Fatalf("testutil: CreateUser credential: %v", err)
	}
	return userID
}

// CreateSession generates a raw session token, stores its hash in the DB,
// and returns the raw token for use in Authorization headers.
func CreateSession(t *testing.T, pool *pgxpool.Pool, userID string) string {
	t.Helper()
	raw := randHex(32)
	expiresAt := time.Now().Add(30 * 24 * time.Hour)
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO sessions (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
		userID, hashRaw(raw), expiresAt,
	); err != nil {
		t.Fatalf("testutil: CreateSession: %v", err)
	}
	return raw
}

// CreateAPIToken generates a raw API token (ptk_ prefix), stores its hash, and
// returns the raw token.
func CreateAPIToken(t *testing.T, pool *pgxpool.Pool, userID, name string) string {
	t.Helper()
	raw := "ptk_" + randHex(32)
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO api_tokens (user_id, name, token_hash) VALUES ($1, $2, $3)`,
		userID, name, hashRaw(raw),
	); err != nil {
		t.Fatalf("testutil: CreateAPIToken: %v", err)
	}
	return raw
}

// CreateTeam inserts a team, adds userID as the given role, and returns the team UUID string.
func CreateTeam(t *testing.T, pool *pgxpool.Pool, userID, role, name string) string {
	t.Helper()
	ctx := context.Background()

	var teamID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO teams (name) VALUES ($1) RETURNING id`, name,
	).Scan(&teamID); err != nil {
		t.Fatalf("testutil: CreateTeam: %v", err)
	}
	AddMember(t, pool, teamID, userID, role)
	return teamID
}

// AddMember inserts a team_memberships row for (teamID, userID, role).
func AddMember(t *testing.T, pool *pgxpool.Pool, teamID, userID, role string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO team_memberships (team_id, user_id, role) VALUES ($1, $2, $3::team_role)`,
		teamID, userID, role,
	); err != nil {
		t.Fatalf("testutil: AddMember: %v", err)
	}
}

// CreatePart inserts a part and returns its UUID string.
// Pass a non-nil teamID only for scope="team".
func CreatePart(t *testing.T, pool *pgxpool.Pool, scope, mpn string, teamID *string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO parts (scope, mpn, team_id) VALUES ($1::part_scope, $2, $3) RETURNING id`,
		scope, mpn, teamID,
	).Scan(&id); err != nil {
		t.Fatalf("testutil: CreatePart: %v", err)
	}
	return id
}

// GetTestDistributorID returns the UUID of the distributor with api_type='test'
// (seeded by migration 000001). Fatal if not found.
func GetTestDistributorID(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(),
		`SELECT id FROM distributors WHERE api_type = 'test' LIMIT 1`,
	).Scan(&id); err != nil {
		t.Fatalf("testutil: GetTestDistributorID: migration 000001 may not have run: %v", err)
	}
	return id
}

// EnableDistributor inserts a distributor_configs row enabling distributorID for teamID.
func EnableDistributor(t *testing.T, pool *pgxpool.Pool, teamID, distributorID string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO distributor_configs (team_id, distributor_id, enabled, preferred)
		 VALUES ($1, $2, TRUE, TRUE)`,
		teamID, distributorID,
	); err != nil {
		t.Fatalf("testutil: EnableDistributor: %v", err)
	}
}

// CreateProject inserts a project and returns its UUID string.
func CreateProject(t *testing.T, pool *pgxpool.Pool, teamID, userID, name string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO projects (team_id, created_by, name) VALUES ($1, $2, $3) RETURNING id`,
		teamID, userID, name,
	).Scan(&id); err != nil {
		t.Fatalf("testutil: CreateProject: %v", err)
	}
	return id
}

// CreateRevision inserts a BOM revision for a project and returns its UUID string.
func CreateRevision(t *testing.T, pool *pgxpool.Pool, projectID, userID, label string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO bom_revisions (project_id, imported_by, label) VALUES ($1, $2, $3) RETURNING id`,
		projectID, userID, label,
	).Scan(&id); err != nil {
		t.Fatalf("testutil: CreateRevision: %v", err)
	}
	return id
}

// SetActiveBOM sets active_bom_revision_id on a project.
func SetActiveBOM(t *testing.T, pool *pgxpool.Pool, projectID, revisionID string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE projects SET active_bom_revision_id = $2 WHERE id = $1`,
		projectID, revisionID,
	); err != nil {
		t.Fatalf("testutil: SetActiveBOM: %v", err)
	}
}

// CreateBOMLineItem inserts a BOM line item and returns its UUID string.
func CreateBOMLineItem(t *testing.T, pool *pgxpool.Pool, revisionID, refDesig string, qty int32) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO bom_line_items (bom_revision_id, reference_designators, quantity)
		 VALUES ($1, $2, $3) RETURNING id`,
		revisionID, refDesig, qty,
	).Scan(&id); err != nil {
		t.Fatalf("testutil: CreateBOMLineItem: %v", err)
	}
	return id
}

// ResolveBOMLineItem sets resolved_part_id on a BOM line item.
func ResolveBOMLineItem(t *testing.T, pool *pgxpool.Pool, itemID, partID string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE bom_line_items SET resolved_part_id = $2 WHERE id = $1`,
		itemID, partID,
	); err != nil {
		t.Fatalf("testutil: ResolveBOMLineItem: %v", err)
	}
}

// CreateInventoryEntry inserts an inventory entry with quantity_on_hand=0 and returns its UUID string.
func CreateInventoryEntry(t *testing.T, pool *pgxpool.Pool, teamID, partID string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO inventory_entries (team_id, part_id) VALUES ($1, $2) RETURNING id`,
		teamID, partID,
	).Scan(&id); err != nil {
		t.Fatalf("testutil: CreateInventoryEntry: %v", err)
	}
	return id
}

// SetStockQty directly sets quantity_on_hand for an inventory entry.
func SetStockQty(t *testing.T, pool *pgxpool.Pool, entryID string, qty int32) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE inventory_entries SET quantity_on_hand = $2 WHERE id = $1`,
		entryID, qty,
	); err != nil {
		t.Fatalf("testutil: SetStockQty: %v", err)
	}
}

// CreateListing inserts a distributor listing with unit_price=0 and returns its UUID string.
func CreateListing(t *testing.T, pool *pgxpool.Pool, partID, distributorID, sku string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO distributor_listings (part_id, distributor_id, sku, packaging_type, unit_price)
		 VALUES ($1, $2, $3, 'tube', 0) RETURNING id`,
		partID, distributorID, sku,
	).Scan(&id); err != nil {
		t.Fatalf("testutil: CreateListing: %v", err)
	}
	return id
}

// CreateOrder inserts a draft order and returns its UUID string.
func CreateOrder(t *testing.T, pool *pgxpool.Pool, teamID, distributorID, userID string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO orders (team_id, distributor_id, created_by) VALUES ($1, $2, $3) RETURNING id`,
		teamID, distributorID, userID,
	).Scan(&id); err != nil {
		t.Fatalf("testutil: CreateOrder: %v", err)
	}
	return id
}

// CreateOrderLineItem inserts an order line item with unit_price_snapshot=0 and returns its UUID string.
func CreateOrderLineItem(t *testing.T, pool *pgxpool.Pool, orderID, listingID string, qty int32) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO order_line_items (order_id, listing_id, quantity_ordered, unit_price_snapshot)
		 VALUES ($1, $2, $3, 0) RETURNING id`,
		orderID, listingID, qty,
	).Scan(&id); err != nil {
		t.Fatalf("testutil: CreateOrderLineItem: %v", err)
	}
	return id
}

// PlaceOrder transitions an order from draft to placed status.
func PlaceOrder(t *testing.T, pool *pgxpool.Pool, orderID string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE orders SET status = 'placed' WHERE id = $1`, orderID,
	); err != nil {
		t.Fatalf("testutil: PlaceOrder: %v", err)
	}
}

// CreateBuild inserts a build in the given status and returns its UUID string.
// status must be a valid build_status enum value (draft, pending, ordered, ready, assembled, cancelled).
func CreateBuild(t *testing.T, pool *pgxpool.Pool, projectID, revID, teamID, userID string, qty int32, status string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO builds (project_id, bom_revision_id, team_id, created_by, quantity, status, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, NOW()) RETURNING id`,
		projectID, revID, teamID, userID, qty, status,
	).Scan(&id); err != nil {
		t.Fatalf("testutil: CreateBuild: %v", err)
	}
	return id
}

// CreateBuildPartAllocation inserts a build_part_allocations row.
func CreateBuildPartAllocation(t *testing.T, pool *pgxpool.Pool, buildID, partID string, needed, reserved, received int32) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO build_part_allocations (build_id, part_id, quantity_needed, quantity_reserved, quantity_received)
		 VALUES ($1, $2, $3, $4, $5)`,
		buildID, partID, needed, reserved, received,
	); err != nil {
		t.Fatalf("testutil: CreateBuildPartAllocation: %v", err)
	}
}

// LinkBuildToOrder inserts a build_orders row linking a build to an order.
func LinkBuildToOrder(t *testing.T, pool *pgxpool.Pool, buildID, orderID string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO build_orders (build_id, order_id) VALUES ($1, $2)`,
		buildID, orderID,
	); err != nil {
		t.Fatalf("testutil: LinkBuildToOrder: %v", err)
	}
}
