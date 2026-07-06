package engine

import (
	"maps"
	"sync"
)

// stepOutputKey is the field under which a step's output is exposed in the run
// context, so a CEL expression reads it as `steps.<id>.output`.
const stepOutputKey = "output"

// RunContext holds the mutable data a workflow run reads and writes through
// CEL. Its data model is fixed:
//
//	trigger              — the event/payload that started the run (immutable)
//	params               — the run's input parameters (immutable)
//	steps.<id>.output    — each activity step's output, keyed by step id
//	vars.<name>          — run variables written by `set` activities
//
// A RunContext is safe for concurrent use: Parallel branches execute in
// separate goroutines that read and write it simultaneously. Reads produce a
// consistent snapshot via activation. trigger and params are established once
// at construction and never mutated, so they are shared without copying;
// steps and vars are mutated (new keys added) and are therefore guarded by a
// RWMutex and shallow-copied on snapshot.
//
// Concurrency contract: step outputs are keyed by unique step id, so parallel
// writes never collide. vars writes are serialized last-write-wins; two
// branches writing the same var concurrently is an authoring mistake, not
// something the engine resolves.
type RunContext struct {
	mu sync.RWMutex

	// trigger and params are immutable after construction; no lock needed to
	// read them, and concurrent map reads are safe.
	trigger map[string]any
	params  map[string]any

	// steps and vars are mutated during the run; all access is under mu.
	steps map[string]any // stepID -> map[string]any{"output": value}
	vars  map[string]any
}

// RunContextConfig configures a new [RunContext]. Both fields are optional; a
// nil map is treated as empty.
type RunContextConfig struct {
	// Trigger is the event or payload that started the run.
	Trigger map[string]any
	// Params are the run's input parameters.
	Params map[string]any
}

// NewRunContext builds a RunContext from cfg. The Trigger and Params maps are
// cloned so later caller mutations can't leak into the run.
func NewRunContext(cfg RunContextConfig) *RunContext {
	return &RunContext{
		trigger: cloneOrEmpty(cfg.Trigger),
		params:  cloneOrEmpty(cfg.Params),
		steps:   map[string]any{},
		vars:    map[string]any{},
	}
}

// SetVar assigns a run variable, readable as `vars.<name>` in CEL. Concurrent
// writes are serialized; the last write wins.
func (rc *RunContext) SetVar(name string, value any) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.vars[name] = value
}

// SetStepOutput records a step's output, readable as `steps.<id>.output` in
// CEL. Each step id is unique within a version, so parallel writes to distinct
// ids never collide.
func (rc *RunContext) SetStepOutput(stepID string, output any) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.steps[stepID] = map[string]any{stepOutputKey: output}
}

// activation returns a consistent CEL input snapshot: {trigger, params, steps,
// vars}. The mutable maps (steps, vars) are shallow-copied under the read lock
// so a concurrent SetVar/SetStepOutput can't mutate the map CEL is ranging
// over. Nested values (a step's output map, a var's value) are written once
// and thereafter only read, so they need no copy.
func (rc *RunContext) activation() map[string]any {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return map[string]any{
		"trigger": rc.trigger, // immutable, safe to share
		"params":  rc.params,  // immutable, safe to share
		"steps":   maps.Clone(rc.steps),
		"vars":    maps.Clone(rc.vars),
	}
}

// VarsSnapshot returns a copy of the current run variables. This is the run's
// output convention (see [Result.Output]).
func (rc *RunContext) VarsSnapshot() map[string]any {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return maps.Clone(rc.vars)
}

func cloneOrEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return maps.Clone(m)
}
