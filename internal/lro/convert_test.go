package lro

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/structpb"

	db "github.com/pivoxai/pivox/internal/db/generated"
)

func TestDbToProto_Pending(t *testing.T) {
	now := time.Now()
	opID := uuid.New()
	dbOp := db.Operation{
		ID:           opID,
		Prefix:       "folders",
		Done:         false,
		Metadata:     nil,
		Result:       nil,
		ErrorCode:    pgtype.Int4{Valid: false},
		ErrorMessage: pgtype.Text{Valid: false},
		ExpireTime:   now.Add(1 * time.Hour),
		CreateTime:   now,
		UpdateTime:   now,
	}

	op, err := dbToProto(dbOp)
	require.NoError(t, err)

	assert.Equal(t, "operations/folders/"+opID.String(), op.Name)
	assert.False(t, op.Done)
	assert.Nil(t, op.GetResponse())
	assert.Nil(t, op.GetError())
}

func TestDbToProto_CompletedWithResult(t *testing.T) {
	now := time.Now()
	opID := uuid.New()

	s, err := structpb.NewStruct(map[string]interface{}{
		"key": "value",
	})
	require.NoError(t, err)

	resultBytes, err := marshalAny(s)
	require.NoError(t, err)

	dbOp := db.Operation{
		ID:           opID,
		Prefix:       "projects",
		Done:         true,
		Metadata:     nil,
		Result:       resultBytes,
		ErrorCode:    pgtype.Int4{Valid: false},
		ErrorMessage: pgtype.Text{Valid: false},
		ExpireTime:   now.Add(1 * time.Hour),
		CreateTime:   now,
		UpdateTime:   now,
	}

	op, err := dbToProto(dbOp)
	require.NoError(t, err)

	assert.Equal(t, "operations/projects/"+opID.String(), op.Name)
	assert.True(t, op.Done)
	assert.NotNil(t, op.GetResponse())
	assert.Nil(t, op.GetError())
}

func TestDbToProto_Failed(t *testing.T) {
	now := time.Now()
	opID := uuid.New()

	dbOp := db.Operation{
		ID:           opID,
		Prefix:       "apikeys",
		Done:         true,
		Metadata:     nil,
		Result:       nil,
		ErrorCode:    pgtype.Int4{Int32: 5, Valid: true},
		ErrorMessage: pgtype.Text{String: "not found", Valid: true},
		ExpireTime:   now.Add(1 * time.Hour),
		CreateTime:   now,
		UpdateTime:   now,
	}

	op, err := dbToProto(dbOp)
	require.NoError(t, err)

	assert.Equal(t, "operations/apikeys/"+opID.String(), op.Name)
	assert.True(t, op.Done)
	assert.Nil(t, op.GetResponse())

	rpcErr := op.GetError()
	require.NotNil(t, rpcErr)
	assert.Equal(t, int32(5), rpcErr.Code)
	assert.Equal(t, "not found", rpcErr.Message)
}

func TestMarshalUnmarshalAny(t *testing.T) {
	s, err := structpb.NewStruct(map[string]interface{}{
		"hello": "world",
	})
	require.NoError(t, err)

	data, err := marshalAny(s)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	var raw map[string]json.RawMessage
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)

	typeURLRaw, ok := raw["@type"]
	require.True(t, ok, "expected @type key in marshaled Any")

	var typeURL string
	err = json.Unmarshal(typeURLRaw, &typeURL)
	require.NoError(t, err)
	assert.Contains(t, typeURL, "google.protobuf.Struct")

	a, err := unmarshalAny(data)
	require.NoError(t, err)
	assert.Contains(t, a.TypeUrl, "google.protobuf.Struct")
}
