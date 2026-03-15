package resource

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSegment(t *testing.T) {
	got, err := ParseSegment("organizations/meridian")
	require.NoError(t, err)
	assert.Equal(t, "meridian", got)

	got, err = ParseSegment("tagKeys/550e8400-e29b-41d4-a716-446655440000")
	require.NoError(t, err)
	assert.Equal(t, "550e8400-e29b-41d4-a716-446655440000", got)

	_, err = ParseSegment("invalid")
	require.Error(t, err)

	_, err = ParseSegment("organizations/")
	require.Error(t, err)
}

func TestCollectionFromName(t *testing.T) {
	assert.Equal(t, "organizations", CollectionFromName("organizations/meridian"))
	assert.Equal(t, "tagKeys", CollectionFromName("tagKeys/some-uuid"))
	assert.Equal(t, "", CollectionFromName(""))
}
