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
//   - Space-scoped, USER_MANAGED dashboards live in the dashboards
//     table and support full CRUD with a SYSTEM_MANAGED-mutation
//     guard for forward compatibility. The guard is data-driven —
//     it reads the row's management_mode column and rejects with
//     FailedPrecondition if SYSTEM_MANAGED, regardless of which URL
//     the request came in on.
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

	pool    db.RWPool
	queries db.Querier
	audit   *audit.Resolver
}

// Config is the constructor input for Server.
type Config struct {
	// Pool is the database pool. Required: space-scoped CRUD wraps
	// each mutation in a transaction so the SYSTEM_MANAGED-mutation
	// guard's read-then-write window can't be raced by a concurrent
	// update.
	Pool db.RWPool

	// Queries is the sqlc query interface. Required.
	Queries db.Querier

	// AuditResolver inflates audit-field UUIDs into Actor protos.
	// Optional; nil leaves Actor fields unset on USER_MANAGED
	// dashboards. SYSTEM_MANAGED dashboards have no audit fields
	// today.
	AuditResolver *audit.Resolver
}

// NewServer constructs the Dashboards service handler. It runs
// boot-time validation against the templates registry and the
// system catalog before returning; any regression in either
// (template with an unknown ListPermission, catalog entry whose
// Build is broken, etc.) panics so operators see a loud startup
// failure rather than a silently broken service.
//
// Required Config fields: Pool, Queries. AuditResolver is optional.
func NewServer(cfg Config) *Server {
	if cfg.Pool == nil {
		panic("dashboards: Config.Pool is required")
	}
	if cfg.Queries == nil {
		panic("dashboards: Config.Queries is required")
	}
	if err := validateRegistries(templates.All(), system.All()); err != nil {
		panic("dashboards: " + err.Error())
	}
	return &Server{
		pool:    cfg.Pool,
		queries: cfg.Queries,
		audit:   cfg.AuditResolver,
	}
}
