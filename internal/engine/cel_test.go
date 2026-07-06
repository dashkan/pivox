package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvaluator_EvalOverContext(t *testing.T) {
	t.Parallel()

	eval, err := NewEvaluator()
	require.NoError(t, err)

	rc := NewRunContext(RunContextConfig{
		Trigger: map[string]any{"name": "world"},
		Params:  map[string]any{"count": int64(2)},
	})
	rc.SetVar("greeting", "hello")
	rc.SetStepOutput("prev", map[string]any{"value": int64(40)})

	tests := []struct {
		name string
		expr string
		want any
	}{
		{name: "string concat", expr: `vars.greeting + " " + trigger.name`, want: "hello world"},
		{name: "arithmetic on params", expr: `params.count * 3`, want: int64(6)},
		{name: "reads step output", expr: `steps.prev.output.value + 2`, want: int64(42)},
		{name: "bool predicate", expr: `params.count == 2`, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := eval.EvalAny(context.Background(), tt.expr, rc)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEvaluator_SecretFunctionFailsToCompile(t *testing.T) {
	t.Parallel()

	eval, err := NewEvaluator()
	require.NoError(t, err)

	rc := NewRunContext(RunContextConfig{})

	// secret() is connector-config-only (6c); it is NOT in the run environment,
	// so referencing it must fail to compile. This is the fence proof.
	_, err = eval.EvalAny(context.Background(), `secret("api_key")`, rc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "compiling CEL expression")
	// A compile failure is terminal, never retryable.
	assert.False(t, IsRetryable(err))
}

func TestEvaluator_EvalStringRejectsNonString(t *testing.T) {
	t.Parallel()

	eval, err := NewEvaluator()
	require.NoError(t, err)
	rc := NewRunContext(RunContextConfig{})

	_, err = eval.EvalString(context.Background(), `1 + 1`, rc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not evaluate to a string")
}

func TestEvaluator_EvalBoolRejectsNonBool(t *testing.T) {
	t.Parallel()

	eval, err := NewEvaluator()
	require.NoError(t, err)
	rc := NewRunContext(RunContextConfig{})

	_, err = eval.EvalBool(context.Background(), `"nope"`, rc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not evaluate to a bool")
}

func TestEvaluator_ConvertsAggregatesToNativeGo(t *testing.T) {
	t.Parallel()

	eval, err := NewEvaluator()
	require.NoError(t, err)
	rc := NewRunContext(RunContextConfig{})

	got, err := eval.EvalAny(context.Background(), `{"a": 1, "b": [2, 3]}`, rc)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"a": int64(1), "b": []any{int64(2), int64(3)}}, got)
}
