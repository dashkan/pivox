package connector

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// httpErrorDetail mirrors the engine's unexported interface. Declaring it here
// pins the method set connector.ResponseError must satisfy so a Try/catch can
// surface error.status / error.body, without engine having to export it.
type httpErrorDetail interface {
	HTTPStatus() int
	HTTPBody() []byte
}

var _ httpErrorDetail = (*ResponseError)(nil)

func TestResponseError_HTTPDetailAccessors(t *testing.T) {
	t.Parallel()

	re := &ResponseError{Status: 404, Body: []byte(`{"error":"not found"}`)}

	var detail httpErrorDetail = re
	assert.Equal(t, 404, detail.HTTPStatus())
	assert.Equal(t, `{"error":"not found"}`, string(detail.HTTPBody()))
}
