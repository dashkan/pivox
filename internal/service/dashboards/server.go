// Copyright 2025 Pivox
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package dashboards implements the gRPC service handler for the
// Dashboards RPC surface. The handler serves two distinct flavors
// of Dashboard:
//
//   - Org-scoped, SYSTEM_MANAGED dashboards (e.g. the org-level
//     Library) are read out of internal/dashboard/system at request
//     time. No DB rows.
//   - Space-scoped, USER_MANAGED dashboards live in the database
//     and support full CRUD. (Implemented in Phase 4b — Phase 4a
//     stubs every space-scoped RPC with Unimplemented.)
//
// At construction time NewServer iterates the templates registry
// and the system catalog through validateRegistries; any wiring
// regression panics the binary at boot rather than silently
// degrading the running service.
package dashboards

import (
	"github.com/dashkan/pivox/internal/audit"
	"github.com/dashkan/pivox/internal/dashboard/system"
	"github.com/dashkan/pivox/internal/dashboard/templates"
	db "github.com/dashkan/pivox/internal/db/generated"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
)

// Server implements the apiv1.DashboardsServer surface.
type Server struct {
	apiv1.UnimplementedDashboardsServer

	// pool / queries are reserved for Phase 4b's space-scoped
	// USER_MANAGED CRUD. They are unused by Phase 4a's org-scoped
	// read-only surface.
	pool    db.RWPool
	queries db.Querier
	audit   *audit.Resolver
}

// Config is the constructor input for Server.
type Config struct {
	// Pool is reserved for Phase 4b. May be nil while only the
	// org-scoped read surface is in use.
	Pool db.RWPool

	// Queries is reserved for Phase 4b. May be nil while only the
	// org-scoped read surface is in use.
	Queries db.Querier

	// AuditResolver inflates audit-field UUIDs into Actor protos.
	// Optional; nil leaves Actor fields unset on USER_MANAGED
	// dashboards. SYSTEM_MANAGED dashboards have no audit fields.
	AuditResolver *audit.Resolver
}

// NewServer constructs the Dashboards service handler. It runs
// boot-time validation against the templates registry and the
// system catalog before returning; any regression in either
// (template with an unknown ListPermission, catalog entry whose
// Build is broken, etc.) panics so operators see a loud startup
// failure rather than a silently broken service.
//
// Required Config fields: none in Phase 4a (Pool / Queries gain
// required-status when Phase 4b adds the space-scoped CRUD path).
func NewServer(cfg Config) *Server {
	if err := validateRegistries(templates.All(), system.All()); err != nil {
		panic("dashboards: " + err.Error())
	}
	return &Server{
		pool:    cfg.Pool,
		queries: cfg.Queries,
		audit:   cfg.AuditResolver,
	}
}
