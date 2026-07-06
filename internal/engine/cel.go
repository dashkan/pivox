package engine

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

// interruptCheckFrequency bounds how many comprehension iterations run between
// context-cancellation checks during CEL evaluation. Workflow expressions are
// small, but this keeps a pathological expression cancellable.
const interruptCheckFrequency = 100

// Evaluator is the single CEL compile+evaluate site for the engine. Every CEL
// expression in a workflow — condition `when`, `set` assignments, and (in 6c)
// activity inputs — goes through one Evaluator so the environment and
// conversion rules live in exactly one place.
//
// The environment declares the four run-context roots (trigger, params, steps,
// vars) as dynamic values and deliberately does NOT declare `secret()`: that
// function is connector-config-only (added in 6c), so any definition that
// references secret() in a condition or assignment fails to compile here.
//
// Compiled programs are cached per expression string. cel.Program is safe for
// concurrent evaluation, so a single Evaluator serves all Parallel branches.
type Evaluator struct {
	env *cel.Env

	mu       sync.Mutex
	programs map[string]cel.Program
}

// NewEvaluator builds an Evaluator over the workflow run environment.
func NewEvaluator() (*Evaluator, error) {
	env, err := buildRunEnv()
	if err != nil {
		return nil, err
	}
	return &Evaluator{
		env:      env,
		programs: map[string]cel.Program{},
	}, nil
}

// buildRunEnv declares the run-context roots. The roots are dynamic because the
// trigger, params, step outputs, and vars are JSON-shaped values whose concrete
// types aren't known until run time. No `secret()` function is declared.
func buildRunEnv() (*cel.Env, error) {
	env, err := cel.NewEnv(
		cel.Variable("trigger", cel.DynType),
		cel.Variable("params", cel.DynType),
		cel.Variable("steps", cel.DynType),
		cel.Variable("vars", cel.DynType),
	)
	if err != nil {
		return nil, fmt.Errorf("engine: building CEL environment: %w", err)
	}
	return env, nil
}

// program returns the compiled, cached program for expr. A compile error is
// terminal (a bad expression does not become valid on retry), so it is returned
// unwrapped — never as a RetryableError.
func (e *Evaluator) program(expr string) (cel.Program, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if prg, ok := e.programs[expr]; ok {
		return prg, nil
	}

	ast, iss := e.env.Compile(expr)
	if iss != nil && iss.Err() != nil {
		return nil, fmt.Errorf("engine: compiling CEL expression %q: %w", expr, iss.Err())
	}

	prg, err := e.env.Program(ast, cel.InterruptCheckFrequency(interruptCheckFrequency))
	if err != nil {
		return nil, fmt.Errorf("engine: building CEL program for %q: %w", expr, err)
	}

	e.programs[expr] = prg
	return prg, nil
}

// EvalAny evaluates expr against rc and returns the result as a plain Go value
// (string, int64, float64, bool, []byte, nil, []any, or map[string]any). Both
// compile and evaluation errors are terminal.
func (e *Evaluator) EvalAny(ctx context.Context, expr string, rc *RunContext) (any, error) {
	prg, err := e.program(expr)
	if err != nil {
		return nil, err
	}

	out, _, err := prg.ContextEval(ctx, rc.activation())
	if err != nil {
		return nil, fmt.Errorf("engine: evaluating CEL expression %q: %w", expr, err)
	}

	native, err := nativeFromVal(out)
	if err != nil {
		return nil, fmt.Errorf("engine: converting CEL result of %q: %w", expr, err)
	}
	return native, nil
}

// EvalString evaluates expr and requires a string result.
func (e *Evaluator) EvalString(ctx context.Context, expr string, rc *RunContext) (string, error) {
	v, err := e.EvalAny(ctx, expr, rc)
	if err != nil {
		return "", err
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("engine: CEL expression %q did not evaluate to a string (got %T)", expr, v)
	}
	return s, nil
}

// EvalBool evaluates expr and requires a bool result. A non-bool result is a
// terminal definition error (e.g. a condition `when` that isn't a predicate).
func (e *Evaluator) EvalBool(ctx context.Context, expr string, rc *RunContext) (bool, error) {
	v, err := e.EvalAny(ctx, expr, rc)
	if err != nil {
		return false, err
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("engine: CEL expression %q did not evaluate to a bool (got %T)", expr, v)
	}
	return b, nil
}

// nativeFromVal converts a CEL result value into a plain, JSON-shaped Go value.
// Scalars round-trip through ref.Val.Value(); aggregates are walked so nested
// CEL map/list types become map[string]any/[]any. The result re-adapts cleanly
// when fed back into CEL as a later step output or var, and is directly
// persistable by 6b.
func nativeFromVal(v ref.Val) (any, error) {
	switch val := v.(type) {
	case traits.Lister:
		return listNative(val)
	case traits.Mapper:
		return mapNative(val)
	}

	if v.Type() == types.NullType {
		return nil, nil
	}
	return v.Value(), nil
}

func listNative(l traits.Lister) ([]any, error) {
	n, ok := l.Size().Value().(int64)
	if !ok {
		return nil, fmt.Errorf("engine: CEL list size was not an integer")
	}
	out := make([]any, 0, n)
	for it := l.Iterator(); it.HasNext() == types.True; {
		elem, err := nativeFromVal(it.Next())
		if err != nil {
			return nil, err
		}
		out = append(out, elem)
	}
	return out, nil
}

func mapNative(m traits.Mapper) (map[string]any, error) {
	out := map[string]any{}
	for it := m.Iterator(); it.HasNext() == types.True; {
		key := it.Next()

		keyNative, err := nativeFromVal(key)
		if err != nil {
			return nil, err
		}

		val, found := m.Find(key)
		if !found {
			continue
		}
		valNative, err := nativeFromVal(val)
		if err != nil {
			return nil, err
		}
		out[fmt.Sprint(keyNative)] = valNative
	}
	return out, nil
}
