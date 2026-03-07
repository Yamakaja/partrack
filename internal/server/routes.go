package server

import (
	"github.com/Yamakaja/partrack/internal/handler"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/go-chi/chi/v5"
)

// registerRoutes mounts all API routes on r under /api/v1.
// Routes match the spec in api.md exactly.
func registerRoutes(r chi.Router, h *handler.Handler, mw *middleware.Middleware) {
	r.Route("/api/v1", func(r chi.Router) {
		// ── 1. Authentication (no session required) ─────────────────────────
		r.Post("/auth/login", h.Login)
		r.Get("/auth/oidc/{provider}", h.OIDCInitiate)
		r.Get("/auth/oidc/{provider}/callback", h.OIDCCallback)
		r.Get("/auth/reset-password", h.GetResetPasswordInfo)
		r.Post("/auth/reset-password", h.CompletePasswordReset)

		// Categories (public — no auth required)
		r.Get("/categories", h.ListCategories)

		// ── Authenticated-only routes ────────────────────────────────────────
		r.Group(func(r chi.Router) {
			r.Use(mw.Authenticate)

			r.Post("/auth/logout", h.Logout)
			r.Get("/auth/me", h.Me)

			// Admin (instance-level)
			r.Group(func(r chi.Router) {
				r.Use(mw.RequireAdmin)
				r.Get("/admin/users", h.ListUsers)
				r.Post("/admin/users", h.CreateUser)
				r.Get("/admin/users/{userId}", h.GetUser)
				r.Patch("/admin/users/{userId}", h.UpdateUser)
				r.Post("/admin/users/{userId}/set-password", h.SetPassword)
				r.Post("/admin/users/{userId}/password-reset-link", h.GeneratePasswordResetLink)
			})

			// User search
			r.Get("/users/search", h.SearchUsers)

			// 2. Session management
			r.Get("/sessions", h.ListSessions)
			r.Delete("/sessions/{sessionId}", h.RevokeSession)

			// 3. API tokens
			r.Get("/tokens", h.ListTokens)
			r.Post("/tokens", h.CreateToken)
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
			r.Get("/parts/{partId}", h.GetPart)
			r.Patch("/parts/{partId}", h.UpdatePart)
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

					// 8. Inventory
					r.Get("/inventory", h.ListInventory)
					r.Get("/inventory/replenish-preview", h.ReplenishPreview)
					r.Get("/inventory/{entryId}", h.GetInventoryEntry)
					r.Get("/inventory/{entryId}/adjustments", h.ListAdjustments)

					// Distributor search
					r.Get("/distributor-configs/{distributorId}/search", h.SearchDistributor)

					// 9. Storage locations
					r.Get("/storage-locations", h.ListStorageLocations)

					// 10. Projects & BOMs
					r.Get("/projects", h.ListProjects)
					r.Get("/projects/{projectId}", h.GetProject)
					r.Get("/projects/{projectId}/revisions", h.ListRevisions)
					r.Get("/projects/{projectId}/revisions/{revId}", h.GetRevision)
					r.Get("/projects/{projectId}/revisions/{revId}/substitutes", h.ListSubstitutes)
					r.Get("/projects/{projectId}/revisions/{revId}/stock-check", h.StockCheck)

					// 13. Orders
					r.Get("/orders", h.ListOrders)
					r.Get("/orders/{orderId}", h.GetOrder)

					// 14. Builds
					r.Get("/builds", h.ListBuilds)
					r.Get("/builds/{buildId}", h.GetBuild)

					// 15. General checkouts
					r.Get("/checkouts", h.ListCheckouts)
					r.Get("/checkouts/{checkoutId}", h.GetCheckout)

					// 16. SSE
					r.Get("/events", h.StreamEvents)
				})

				// Member+
				r.Group(func(r chi.Router) {
					r.Use(mw.RequireTeamRole("member"))

					// Inventory
					r.Post("/inventory", h.CreateInventoryEntry)
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

					// Parts (team-scoped mutations)
					r.Post("/parts/enrich-all", h.ReEnrichAll)
					r.Post("/parts/{partId}/enrich", h.EnrichPart)
					r.Post("/parts/{partId}/listings", h.AddListing)
					r.Delete("/parts/{partId}/listings/{listingId}", h.DeleteListing)
					r.Post("/parts/{partId}/listings/refresh", h.RefreshAllListings)
					r.Post("/parts/{partId}/listings/{listingId}/refresh", h.RefreshListing)

					// Builds
					r.Post("/builds/preview", h.PreviewBuild)
					r.Post("/builds", h.CreateBuild)
					r.Post("/builds/{buildId}/submit", h.SubmitBuild)
					r.Post("/builds/{buildId}/checkout", h.CheckoutBuild)
					r.Patch("/builds/{buildId}", h.UpdateBuild)
					r.Delete("/builds/{buildId}", h.CancelBuild)

					// Checkouts
					r.Post("/checkouts", h.CreateCheckout)
				})

				// Purchaser+
				r.Group(func(r chi.Router) {
					r.Use(mw.RequireTeamRole("purchaser"))

					// Cart
					r.Get("/cart", h.GetCart)
					r.Delete("/cart", h.ClearCart)
					r.Post("/cart/items", h.AddCartItem)
					r.Patch("/cart/items/{itemId}", h.UpdateCartItem)
					r.Delete("/cart/items/{itemId}", h.RemoveCartItem)
					r.Post("/cart/add-low-stock", h.AddLowStock)
					r.Post("/cart/convert", h.ConvertCart)

					// Build cart-inclusion toggle (purchaser+)
					r.Patch("/builds/{buildId}/cart-inclusion", h.ToggleBuildCartInclusion)

					// Orders
					r.Patch("/orders/{orderId}", h.UpdateOrder)
					r.Delete("/orders/{orderId}", h.CancelOrder)
					r.Get("/orders/{orderId}/export", h.ExportOrder)
					r.Post("/orders/{orderId}/submit", h.SubmitOrder)
					r.Post("/orders/{orderId}/reference", h.LinkOrderReference)
					r.Post("/orders/{orderId}/items/{itemId}/receive", h.ReceiveOrderItem)
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
