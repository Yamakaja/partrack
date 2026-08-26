package server

import (
	"net/http"
	"time"

	"github.com/Yamakaja/partrack/internal/handler"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httprate"
)

// registerRoutes mounts all API routes on r under /api/v1.
// Routes match the spec in api.md exactly.
func registerRoutes(r chi.Router, h *handler.Handler, mw *middleware.Middleware) {
	r.Get("/api/v1/health", h.Health)
	r.Get("/api/v1/ready", h.Ready)
	r.Get("/api/v1/version", h.Version)
	r.Get("/api/v1/changelog", h.Changelog)

	// authLimiter throttles unauthenticated credential endpoints per client IP to
	// blunt brute-force / password-spray / reset-token guessing (and the bcrypt
	// CPU-DoS that rides on them). Shares one per-IP budget across these routes.
	// A non-positive configured limit disables it (used in tests).
	authLimiter := func(next http.Handler) http.Handler { return next }
	if n := h.Config.Auth.RateLimitPerMinute; n > 0 {
		authLimiter = httprate.LimitByIP(n, time.Minute)
	}

	r.Route("/api/v1", func(r chi.Router) {
		// ── 1. Authentication (no session required) ─────────────────────────
		r.With(authLimiter).Post("/auth/login", h.Login)
		r.Get("/auth/providers", h.OIDCProviders)
		r.With(authLimiter).Get("/auth/oidc/{provider}", h.OIDCInitiate)
		r.With(authLimiter).Get("/auth/oidc/{provider}/callback", h.OIDCCallback)
		r.With(authLimiter).Get("/auth/reset-password", h.GetResetPasswordInfo)
		r.With(authLimiter).Post("/auth/reset-password", h.CompletePasswordReset)

		// ── OAuth callbacks (no session required) ───────────────────────────
		r.Get("/oauth/callback/{distributorApiType}", h.OAuthCallback)

		// Categories and parameter definitions (public — no auth required)
		r.Get("/categories", h.ListCategories)
		r.Get("/parameter-definitions", h.ListParameterDefinitions)

		// ── Phone scan endpoints (scan token auth, no team role) ─────────────
		// StreamScanSession reads the token from ?token= query param directly;
		// it bypasses Authenticate middleware because EventSource can't set headers.
		r.Get("/scan/stream", h.StreamScanSession)
		r.With(authLimiter).Post("/scan/pair", h.PairScanSession)
		r.Group(func(r chi.Router) {
			r.Use(mw.Authenticate)
			r.Post("/scan/stream-ticket", h.CreateStreamTicket)
			r.Get("/scan/session", h.GetScanSession)
			r.Post("/scan/scans", h.SubmitScan)
			r.Post("/scan/confirm", h.ConfirmScan)
			r.Post("/scan/skip", h.SkipScan)
			r.Get("/scan/orders", h.ListScanOrders)
			r.Get("/scan/parts", h.SearchScanParts)
			r.Get("/scan/parts/{partId}", h.GetScanPartDetail)
			r.Post("/scan/parts/import", h.ImportScanPart)
			r.Post("/scan/parts/import-multi", h.ImportScanPartMulti)
			r.Post("/scan/stock-adjustment", h.CreateScanStockAdjustment)
			r.Post("/scan/lookup", h.LookupScanBarcode)
			r.Get("/scan/distributor-search", h.SearchScanDistributor)
			r.Get("/scan/storage-locations", h.ListScanStorageLocations)
			r.Get("/scan/storage/{locationId}/inventory", h.ListScanStorageInventory)
			r.Patch("/scan/inventory-entries/{id}", h.PatchScanInventoryEntry)
			r.Post("/scan/focus", h.ScanFocus)
			// Bulk scan (phone side)
			r.Post("/scan/bulk/start", h.StartBulkSession)
			r.Post("/scan/bulk/sessions/{sessionId}/items", h.AddBulkScanItem)
			r.Patch("/scan/bulk/sessions/{sessionId}/items/{itemId}", h.PatchBulkScanItem)
			r.Post("/scan/bulk/sessions/{sessionId}/close", h.CloseBulkScanSessionHandler)
			r.Post("/scan/bulk/sessions/{sessionId}/navigate", h.NavigateBulkSession)
			// Collect mode (phone side) — scan bags into a part collection
			r.Get("/scan/collections", h.ListScanCollections)
			r.Post("/scan/collections", h.CreateScanCollection)
			r.Patch("/scan/collections/{collectionId}", h.UpdateScanCollection)
			r.Get("/scan/collections/{collectionId}/items", h.ListScanCollectionItems)
			r.Post("/scan/collect", h.CollectScan)
			// Filter mode (phone side) — scan bags against a target set (read-only)
			r.Post("/scan/check", h.CheckScan)
			r.Post("/scan/check/target", h.CheckTarget)
			r.Post("/scan/check/finish", h.CheckFinish)
			r.Get("/scan/builds", h.ListScanBuilds)
			r.Get("/scan/kitting/{kittingSessionId}", h.GetKittingSessionHandler)
			r.Get("/scan/kitting/{kittingSessionId}/items", h.GetKittingItemsHandler)
			r.Post("/scan/kitting/{kittingSessionId}/confirm", h.KittingConfirm)
			r.Post("/scan/kitting/{kittingSessionId}/shortfall", h.KittingShortfall)
			r.Post("/scan/kitting/{kittingSessionId}/complete", h.KittingComplete)
			r.Post("/scan/kitting/{kittingSessionId}/abandon", h.KittingAbandon)
		})

		// ── Authenticated-only routes ────────────────────────────────────────
		r.Group(func(r chi.Router) {
			r.Use(mw.Authenticate)

			r.Post("/auth/logout", h.Logout)
			r.Get("/auth/me", h.Me)

			// Blob bytes: capability-by-hash (any authenticated user; the
			// content address is the bearer token). No team check, no DB lookup.
			r.Get("/blobs/{sha256}", h.ServeBlob)
			r.Head("/blobs/{sha256}", h.ServeBlob)

			// BOM interpretation preview: stateless parse of mapped rows into
			// structured interpretations (no persistence, no team scope).
			r.Post("/bom/interpret-preview", h.InterpretPreview)

			// Admin (instance-level)
			r.Group(func(r chi.Router) {
				r.Use(mw.RequireAdmin)

				// Observability: in-memory request/query timing snapshot.
				r.Get("/admin/metrics", h.AdminMetrics)

				// LDAP/AD configuration diagnostics (read-only; performs a live
				// directory bind, so the bind step rides the auth rate limiter).
				r.Get("/admin/ldap/providers", h.AdminLDAPProviders)
				r.With(authLimiter).Post("/admin/ldap/test-connection", h.AdminLDAPTestConnection)
				r.With(authLimiter).Post("/admin/ldap/diagnose", h.AdminLDAPDiagnose)
				r.With(authLimiter).Post("/admin/ldap/inspect", h.AdminLDAPInspect)

				r.Get("/admin/users", h.ListUsers)
				r.Post("/admin/users", h.CreateUser)
				r.Get("/admin/users/{userId}", h.GetUser)
				r.Patch("/admin/users/{userId}", h.UpdateUser)
				r.Post("/admin/users/{userId}/set-password", h.SetPassword)
				r.Post("/admin/users/{userId}/password-reset-link", h.GeneratePasswordResetLink)

				// Parameter registry management
				r.Get("/admin/parameter-definitions", h.AdminListParameterDefinitions)
				r.Post("/admin/parameter-definitions", h.AdminCreateParameterDefinition)
				r.Patch("/admin/parameter-definitions/{id}", h.AdminUpdateParameterDefinition)
				r.Delete("/admin/parameter-definitions/{id}", h.AdminDeleteParameterDefinition)
				r.Get("/admin/distributor-parameter-mappings", h.AdminListDistributorParamMappings)
				r.Get("/admin/distributor-parameter-mappings/{id}/parse-preview", h.AdminParseMappingPreview)
				r.Patch("/admin/distributor-parameter-mappings/{id}", h.AdminUpdateDistributorParamMapping)

				// Per-category class profile (value_param + ordered primary_params)
				r.Patch("/admin/categories/{id}/params", h.AdminUpdateCategoryParams)
			})

			// Category parameters (authenticated)
			r.Get("/categories/{categoryId}/parameters", h.GetCategoryParameters)

			// User search
			r.Get("/users/search", h.SearchUsers)

			// 2. Session management
			r.Get("/sessions", h.ListSessions)
			r.Delete("/sessions/{sessionId}", h.RevokeSession)

			// 3. API tokens
			r.Get("/tokens", h.ListTokens)
			r.With(authLimiter).Post("/tokens", h.CreateToken)
			r.Delete("/tokens/{tokenId}", h.RevokeToken)

			// 4. User preferences
			r.Get("/preferences", h.GetPreferences)
			r.Put("/preferences", h.PutPreferences)

			// 5. Teams (listing is user-scoped; creation is admin-only)
			r.Get("/teams", h.ListTeams)
			r.Group(func(r chi.Router) {
				r.Use(mw.RequireAdmin)
				r.Post("/teams", h.CreateTeam)
			})

			// 6. Distributors (global read)
			r.Get("/distributors", h.ListDistributors)
			r.Get("/distributors/{distributorId}/credential-spec", h.GetDistributorCredentialSpec)

			// 7. Parts catalog (global + team-scoped reads)
			r.Get("/parts", h.ListParts)
			r.Post("/parts", h.CreatePart)
			r.Post("/parts/generic", h.CreateGenericPart)
			r.Post("/parts/generic/batch", h.BatchCreateGenericParts)
			r.Get("/parts/{partId}", h.GetPart)
			r.Patch("/parts/{partId}", h.UpdatePart)
			r.With(mw.RequireAdmin).Delete("/parts/{partId}", h.DeletePart)
			r.Get("/parts/{partId}/parameters", h.ListParameters)
			r.Post("/parts/{partId}/parameters", h.CreateParameter)
			r.Delete("/parts/{partId}/parameters/{parameterId}", h.DeleteParameter)
			r.Get("/parts/{partId}/listings", h.ListListings)

			// ── Team-scoped routes (require team membership) ─────────────────
			r.Route("/teams/{teamId}", func(r chi.Router) {
				// Viewer+ (read-only)
				r.Group(func(r chi.Router) {
					r.Use(mw.RequireTeamRole("viewer"))

					r.Get("/", h.GetTeam)
					r.Get("/dashboard", h.GetDashboard)
					r.Get("/parts/categories", h.ListPartCategories)
					// Per-category parameter schema (labels, value types, enum vocab)
					// for the BOM interpretation review UI.
					r.Get("/categories/{categoryId}/parameter-schema", h.CategoryParameterSchema)

					// 8. Inventory
					r.Get("/inventory", h.ListInventory)
					r.Get("/inventory/replenish-preview", h.ReplenishPreview)
					r.Get("/inventory/{entryId}", h.GetInventoryEntry)
					r.Get("/inventory/{entryId}/adjustments", h.ListAdjustments)

					// Distributor search
					r.Get("/distributor-configs/{distributorId}/search", h.SearchDistributor)

					// 9. Storage locations
					r.Get("/storage-locations", h.ListStorageLocations)
					r.Get("/storage-locations/{locationId}", h.GetStorageLocation)
					r.Get("/storage-locations/{locationId}/inventory", h.ListInventoryByLocation)

					// 10. Projects & BOMs
					r.Get("/projects", h.ListProjects)
					r.Get("/projects/{projectId}", h.GetProject)
					r.Get("/projects/{projectId}/revisions", h.ListRevisions)
					r.Get("/projects/{projectId}/revisions/{revId}", h.GetRevision)
					r.Get("/projects/{projectId}/revisions/{revId}/substitutes", h.ListSubstitutes)
					r.Get("/projects/{projectId}/revisions/{revId}/items/{itemId}/substitutes", h.ListLineItemSubstitutes)
					r.Get("/projects/{projectId}/revisions/{revId}/items/{itemId}/generic-candidates", h.GenericPartCandidates)
					r.Get("/projects/{projectId}/revisions/{revId}/items/{itemId}/substitutes/previous-revisions", h.ListPreviousRevisionsWithSubstitutes)
					r.Get("/projects/{projectId}/revisions/{revId}/stock-check", h.StockCheck)
					// Project-scoped interpretation memory (doc/bom-import.md §9.2): inspect
					// the remembered BOM-line corrections that future imports will reuse.
					r.Get("/projects/{projectId}/interpretation-memory", h.ListInterpretationMemory)

					// What would importing this BOM do? Walks the real resolution tier
					// ladder (internal/bomresolve, shared with the import job) against
					// the catalog and reports the outcome per line. A POST because the
					// BOM travels in the body; it is read-only and writes nothing,
					// hence viewer rather than member.
					r.Post("/bom/resolve-preview", h.ResolvePreview)

					// 13. Orders
					r.Get("/orders", h.ListOrders)
					r.Get("/orders/{orderId}", h.GetOrder)
					r.Get("/orders/{orderId}/linked-builds", h.GetOrderLinkedBuilds)
					r.Get("/orders/{orderId}/distributor-status", h.GetDistributorStatus)

					// 14. Builds
					r.Get("/builds", h.ListBuilds)
					r.Get("/builds/pipeline", h.GetBuildPipeline)
					r.Get("/builds/{buildId}", h.GetBuild)
					r.Get("/builds/{buildId}/preview", h.GetBuildPreview)

					// Part allocations
					r.Get("/parts/{partId}/allocations", h.GetPartAllocations)

					// Notes (read; write is member+ below)
					r.Get("/parts/{partId}/note", h.GetPartNote)
					r.Get("/projects/{projectId}/note", h.GetProjectNote)

					// Documents (file attachments on parts/projects; read +
					// serve. Serving is access-controlled + filename-aware, unlike
					// the capability-by-hash /blobs path.)
					r.Get("/parts/{partId}/documents", h.ListPartDocuments)
					r.Get("/projects/{projectId}/documents", h.ListProjectDocuments)
					r.Get("/documents/{docId}/content", h.ServeDocument)
					r.Head("/documents/{docId}/content", h.ServeDocument)

					// 15. General checkouts
					r.Get("/checkouts", h.ListCheckouts)
					r.Get("/checkouts/{checkoutId}", h.GetCheckout)

					// 16. SSE
					r.Get("/events", h.StreamEvents)

					// 17. Global search
					r.Get("/search", h.GlobalSearch)

					// 18. Activity log (viewer+; ?scope=team gated to purchaser+ in handler)
					r.Get("/activity-log", h.ListActivity)

					// 19. Batch object-ref resolution (viewer+); ?part=id,id&order=id…
					r.Get("/object-refs", h.ListObjectRefs)

					// 20. Kitting session read (viewer+)
					r.Get("/kitting/{kittingSessionId}", h.GetKittingSessionDesktop)
					r.Get("/kitting/{kittingSessionId}/items", h.GetKittingItemsDesktop)
				})

				// Member+
				r.Group(func(r chi.Router) {
					r.Use(mw.RequireTeamRole("member"))

					// Attachments (image uploads for notes etc.)
					r.Post("/attachments", h.UploadAttachment)

					// Notes + part flag (write)
					r.Put("/parts/{partId}/note", h.PutPartNote)
					r.Put("/parts/{partId}/flag", h.PutPartFlag)
					r.Put("/projects/{projectId}/note", h.PutProjectNote)

					// Documents (upload + delete)
					r.Post("/parts/{partId}/documents", h.UploadPartDocument)
					r.Post("/projects/{projectId}/documents", h.UploadProjectDocument)
					r.Delete("/documents/{docId}", h.DeleteDocument)

					// Inventory
					r.Post("/inventory", h.CreateInventoryEntry)
					r.Post("/inventory/bulk-assign-storage", h.BulkAssignStorage)
					r.Patch("/inventory/{entryId}", h.UpdateInventoryEntry)
					r.Post("/inventory/{entryId}/adjustments", h.CreateAdjustment)

					// Storage locations
					r.Post("/storage-locations", h.CreateStorageLocation)
					r.Patch("/storage-locations/{locationId}", h.UpdateStorageLocation)
					r.Delete("/storage-locations/{locationId}", h.DeleteStorageLocation)

					// Projects
					r.Post("/projects", h.CreateProject)
					r.Patch("/projects/{projectId}", h.UpdateProject)
					r.Post("/projects/{projectId}/revisions", h.ImportRevision)
					r.Patch("/projects/{projectId}/revisions/{revId}", h.UpdateRevision)
					r.Patch("/projects/{projectId}/revisions/{revId}/items/{itemId}", h.UpdateBOMItem)
					r.Post("/projects/{projectId}/revisions/{revId}/items/{itemId}/enrich", h.EnrichBOMItem)
					r.Post("/projects/{projectId}/revisions/{revId}/items/{itemId}/substitutes", h.AddSubstitute)
					r.Delete("/projects/{projectId}/revisions/{revId}/items/{itemId}/substitutes/{partId}", h.RemoveSubstitute)
					r.Put("/projects/{projectId}/revisions/{revId}/items/{itemId}/substitutes/order", h.ReorderSubstitutes)
					r.Post("/projects/{projectId}/revisions/{revId}/items/{itemId}/substitutes/import", h.ImportSubstitutes)
					r.Delete("/projects/{projectId}/interpretation-memory/{memoryId}", h.DeleteInterpretationMemoryEntry)
					r.Patch("/builds/{buildId}/substitute-selections", h.PatchBuildSubstituteSelections)
					r.Post("/builds/{buildId}/substitute-selections/refresh", h.RefreshBuildSubstituteSelections)

					// Part resolution queue
					r.Post("/parts/resolve", h.CreatePartResolveItem)
					r.Get("/parts/resolve/{resolveItemId}", h.GetPartResolveItem)
					r.Post("/parts/resolve/{resolveItemId}/commit", h.CommitPartResolveItem)
					r.Post("/parts/resolve/{resolveItemId}/retry-distributors", h.RetryPartResolveDistributors)

					// Parts (team-scoped mutations)
					r.Post("/parts/import", h.ImportPart)
					r.Post("/parts/refresh-parameters", h.RefreshSearchParameters)
					r.Post("/parts/{partId}/enrich", h.EnrichPart)
					r.Post("/parts/{partId}/listings", h.AddListing)
					r.Delete("/parts/{partId}/listings/{listingId}", h.DeleteListing)
					r.Post("/parts/refresh-listings", h.RefreshSearchListings)
					r.Post("/parts/{partId}/listings/refresh", h.RefreshAllListings)
					r.Post("/parts/{partId}/listings/{listingId}/refresh", h.RefreshListing)

					// Builds
					r.Post("/builds", h.CreateBuild)
					r.Post("/builds/{buildId}/submit", h.SubmitBuild)
					r.Post("/builds/{buildId}/checkout", h.CheckoutBuild)
					r.Patch("/builds/{buildId}", h.UpdateBuild)
					r.Delete("/builds/{buildId}", h.CancelBuild)
					r.Put("/builds/{buildId}/excluded-designators", h.SetBuildExcludedDesignators)
					r.Patch("/builds/{buildId}/line-item-overrides/{itemId}", h.SetBuildLineItemOverride)

					// Checkouts
					r.Post("/checkouts", h.CreateCheckout)

					// Part collections ("Lists") — user-private within the team;
					// every handler filters to the acting user.
					r.Get("/collections", h.ListCollections)
					r.Post("/collections", h.CreateCollection)
					r.Get("/collections/{collectionId}", h.GetCollection)
					r.Patch("/collections/{collectionId}", h.UpdateCollection)
					r.Delete("/collections/{collectionId}", h.DeleteCollection)
					r.Post("/collections/{collectionId}/items", h.AddCollectionItem)
					r.Post("/collections/{collectionId}/items/bulk", h.BulkAddCollectionItems)
					r.Post("/collections/{collectionId}/items/from-search", h.AddSearchResultsToCollection)
					r.Delete("/collections/{collectionId}/items", h.ClearCollectionItems)
					r.Patch("/collections/{collectionId}/items/{itemId}", h.UpdateCollectionItem)
					r.Delete("/collections/{collectionId}/items/{itemId}", h.DeleteCollectionItem)

					// Scan sessions (member+) — literal path before wildcard
					r.Post("/scan-sessions", h.CreateScanSession)
					r.Get("/scan-sessions/active", h.GetActiveScanSession)
					r.Post("/scan-sessions/{sessionId}/pairing-ticket", h.RefreshPairingTicket)
					// Context push is member+: sessions are per-user, and list
					// (Collect) contexts are member-level — the old purchaser
					// gate was inconsistent with StartKitting below.
					r.Patch("/scan-sessions/{sessionId}/context", h.PushScanContext)

					// Build kitting (member+)
					r.Post("/scan-sessions/{sessionId}/kitting", h.StartKitting)

					// Bulk scan sessions (member+)
					r.Get("/bulk-scan-sessions", h.ListBulkScanSessions)
					r.Get("/bulk-scan-badge", h.GetBulkScanBadgeCount)
					r.Get("/bulk-scan-sessions/{sessionId}/items", h.ListBulkScanItemsHandler)
					r.Patch("/bulk-scan-sessions/{sessionId}/items/{itemId}", h.SetBulkScanItemStatusHandler)
					r.Post("/bulk-scan-sessions/{sessionId}/items/{itemId}/retry", h.RetryBulkScanItemHandler)
					r.Post("/bulk-scan-sessions/{sessionId}/items/{itemId}/retry-distributors", h.RetryBulkScanItemDistributorsHandler)
					r.Delete("/bulk-scan-sessions/{sessionId}", h.DeleteBulkScanSessionHandler)
				})

				// Purchaser+
				r.Group(func(r chi.Router) {
					r.Use(mw.RequireTeamRole("purchaser"))

					// Scan session management (purchaser can delete)
					r.Delete("/scan-sessions/{sessionId}", h.DeleteScanSession)

					// Cart
					r.Get("/cart/count", h.GetCartCount)
					r.Get("/cart/part-quantity", h.GetCartPartQuantity)
					r.Get("/cart", h.GetCart)
					r.Delete("/cart", h.ClearCart)
					r.Post("/cart/items", h.AddCartItem)
					r.Post("/cart/items/bulk", h.BulkAddCartItems)
					r.Patch("/cart/items/{itemId}", h.UpdateCartItem)
					r.Delete("/cart/items/{itemId}", h.RemoveCartItem)
					r.Post("/cart/add-low-stock", h.AddLowStock)
					r.Post("/cart/refresh-listings", h.RefreshCartListings)
					r.Post("/cart/convert", h.ConvertCart)

					// Distributor sourcing policy (ranked preference + fallback)
					r.Get("/sourcing-policy", h.GetSourcingPolicy)
					r.Put("/sourcing-policy", h.UpdateSourcingPolicy)

					// Build cart-inclusion toggle (purchaser+)
					r.Patch("/builds/{buildId}/cart-inclusion", h.ToggleBuildCartInclusion)

					// Orders
					r.Post("/orders/import", h.ImportOrder)
					r.Patch("/orders/{orderId}", h.UpdateOrder)
					r.Delete("/orders/{orderId}", h.CancelOrder)
					r.Get("/orders/{orderId}/export", h.ExportOrder)
					r.Post("/orders/{orderId}/submit", h.SubmitOrder)
					r.Post("/orders/{orderId}/reference", h.LinkOrderReference)
					r.Post("/orders/{orderId}/items/{itemId}/receive", h.ReceiveOrderItem)
					r.Post("/orders/{orderId}/push-to-distributor", h.PushToDistributor)
					r.Post("/orders/{orderId}/request-quote", h.RequestQuote)
					r.Post("/orders/{orderId}/link-commit", h.LinkCommit)
					r.Post("/orders/{orderId}/retry-unresolved", h.RetryUnresolvedItems)
					r.Post("/oauth/{distributorApiType}/initiate", h.InitiateOAuth)
					r.Get("/oauth/{distributorApiType}/status", h.GetOAuthStatus)
					r.Delete("/oauth/{distributorApiType}/token", h.DisconnectOAuth)
				})

				// Admin+
				r.Group(func(r chi.Router) {
					r.Use(mw.RequireTeamRole("admin"))

					r.Patch("/", h.UpdateTeam)

					// Members
					r.Get("/members", h.ListMembers)
					r.Post("/members", h.AddMember)
					r.Patch("/members/{userId}", h.UpdateMember)
					r.Delete("/members/{userId}", h.RemoveMember)
					r.Post("/members/{userId}/deactivate", h.DeactivateUser)

					// Distributor configs
					r.Get("/distributor-configs", h.ListDistributorConfigs)
					r.Put("/distributor-configs/{distributorId}", h.UpsertDistributorConfig)
					r.Delete("/distributor-configs/{distributorId}", h.DeleteDistributorConfig)
				})
			})
		})
	})
}
