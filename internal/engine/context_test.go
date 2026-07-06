package engine

import (
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRunContext_ActivationSnapshot(t *testing.T) {
	t.Parallel()

	rc := NewRunContext(RunContextConfig{
		Trigger: map[string]any{"t": 1},
		Params:  map[string]any{"p": 2},
	})
	rc.SetVar("v", "x")
	rc.SetStepOutput("s1", "out")

	act := rc.activation()
	assert.Equal(t, map[string]any{"t": 1}, act["trigger"])
	assert.Equal(t, map[string]any{"p": 2}, act["params"])
	assert.Equal(t, map[string]any{"v": "x"}, act["vars"])
	assert.Equal(t, map[string]any{"s1": map[string]any{"output": "out"}}, act["steps"])

	// A later write must not mutate an already-taken snapshot.
	rc.SetVar("v2", "y")
	assert.Equal(t, map[string]any{"v": "x"}, act["vars"], "snapshot must be stable after later writes")
}

func TestRunContext_InputsCloned(t *testing.T) {
	t.Parallel()

	trigger := map[string]any{"k": "v"}
	rc := NewRunContext(RunContextConfig{Trigger: trigger})

	// Mutating the caller's map after construction must not leak in.
	trigger["k"] = "mutated"
	assert.Equal(t, map[string]any{"k": "v"}, rc.activation()["trigger"])
}

// TestRunContext_ConcurrentAccess exercises the mutex under -race: many
// goroutines write distinct step outputs and vars while others snapshot.
func TestRunContext_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	rc := NewRunContext(RunContextConfig{})
	const n = 100

	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := strconv.Itoa(i)
			rc.SetStepOutput("step-"+id, id)
			rc.SetVar("var-"+id, id)
			_ = rc.activation()
			_ = rc.VarsSnapshot()
		}()
	}
	wg.Wait()

	assert.Len(t, rc.VarsSnapshot(), n)
}
