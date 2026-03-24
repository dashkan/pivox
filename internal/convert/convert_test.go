package convert

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"

	db "github.com/dashkan/pivox/internal/db/generated"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
)

func TestProjectToProto(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	updated := now.Add(1 * time.Hour)

	labels := map[string]string{"env": "prod"}
	labelsJSON, _ := json.Marshal(labels)

	p := db.Project{
		ID:          uuid.New(),
		OrgID:       uuid.New(),
		Name:        "my-project",
		DisplayName: "My Project",
		State:       db.ResourceStateACTIVE,
		Etag:        "etag-proj",
		Labels:      labelsJSON,
		Revision:    1,
		CreateTime:  now,
		UpdateTime:  updated,
		DeleteTime:  pgtype.Timestamptz{Valid: false},
	}

	proto := ProjectToProto(p, "my-org")

	assert.Equal(t, "organizations/my-org/projects/my-project", proto.Name)
	assert.Equal(t, "My Project", proto.DisplayName)
	assert.Equal(t, apiv1.Project_ACTIVE, proto.State)
	assert.Equal(t, map[string]string{"env": "prod"}, proto.Labels)
}

func TestOrganizationToProto(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	updated := now.Add(1 * time.Hour)

	o := db.Organization{
		ID:          uuid.New(),
		Name:        "my-org",
		DisplayName: "My Org",
		State:       db.ResourceStateACTIVE,
		Etag:        "etag-org",
		Revision:    1,
		CreateTime:  now,
		UpdateTime:  updated,
		DeleteTime:  pgtype.Timestamptz{Valid: false},
	}

	proto := OrganizationToProto(o)

	assert.Equal(t, "organizations/my-org", proto.Name)
	assert.Equal(t, "My Org", proto.DisplayName)
	assert.Equal(t, apiv1.Organization_ACTIVE, proto.State)
}

func TestTagKeyToProto(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	updated := now.Add(1 * time.Hour)

	id := uuid.MustParse("0192a000-0004-7000-8000-000000410001")
	tk := db.TagKey{
		ID:             id,
		OrgID:          uuid.New(),
		ShortName:      "env",
		NamespacedName: "org-uuid/env",
		Description:    "Environment tag",
		Etag:           "etag-tk",
		Revision:       1,
		CreateTime:     now,
		UpdateTime:     updated,
	}

	proto := TagKeyToProto(tk)

	assert.Equal(t, "tagKeys/0192a000-0004-7000-8000-000000410001", proto.Name)
	assert.Equal(t, "Environment tag", proto.Description)
}

func TestTagValueToProto(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	updated := now.Add(1 * time.Hour)

	id := uuid.MustParse("0192a000-0005-7000-8000-000000510001")
	tagKeyID := uuid.MustParse("0192a000-0004-7000-8000-000000410001")
	tv := db.TagValue{
		ID:             id,
		TagKeyID:       tagKeyID,
		ShortName:      "production",
		NamespacedName: "org/env/production",
		Description:    "Production environment",
		Etag:           "etag-tv",
		Revision:       1,
		CreateTime:     now,
		UpdateTime:     updated,
	}

	proto := TagValueToProto(tv)

	assert.Equal(t, "tagKeys/0192a000-0004-7000-8000-000000410001/tagValues/0192a000-0005-7000-8000-000000510001", proto.Name)
	assert.Equal(t, "Production environment", proto.Description)
}

func TestTagBindingToProto(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)

	tbID := uuid.MustParse("0192a000-0006-7000-8000-000000610001")
	tvID := uuid.MustParse("0192a000-0005-7000-8000-000000510001")
	tkID := uuid.MustParse("0192a000-0004-7000-8000-000000410001")

	tb := db.TagBinding{
		ID:             tbID,
		ParentResource: "//pivox.api/organizations/meridian/projects/corp-site",
		TagValueID:     tvID,
		Etag:           "etag-tb",
		CreateTime:     now,
		UpdateTime:     now,
	}
	tv := db.TagValue{
		ID:       tvID,
		TagKeyID: tkID,
	}

	proto := TagBindingToProto(tb, tv)

	assert.Equal(t, "tagBindings/0192a000-0006-7000-8000-000000610001", proto.Name)
	assert.Equal(t, "tagKeys/0192a000-0004-7000-8000-000000410001/tagValues/0192a000-0005-7000-8000-000000510001", proto.TagValue)
}

func TestEffectiveTagToProto(t *testing.T) {
	tvID := uuid.MustParse("0192a000-0005-7000-8000-000000510001")
	tkID := uuid.MustParse("0192a000-0004-7000-8000-000000410001")

	row := db.ListEffectiveTagsRow{
		TagValueID:             tvID,
		TagValueNamespacedName: "org/env/production",
		TagKeyID:               tkID,
		TagKeyNamespacedName:   "org/env",
	}

	proto := EffectiveTagToProto(row)

	assert.Equal(t, "tagKeys/0192a000-0004-7000-8000-000000410001/tagValues/0192a000-0005-7000-8000-000000510001", proto.TagValue)
	assert.Equal(t, "tagKeys/0192a000-0004-7000-8000-000000410001", proto.TagKey)
}

func TestApiKeyToProto(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	updated := now.Add(1 * time.Hour)

	annotations := map[string]string{"created-by": "terraform"}
	annotationsJSON, _ := json.Marshal(annotations)

	k := db.ApiKey{
		ID:          uuid.New(),
		OrgID:       uuid.New(),
		KeyID:       "my-key",
		DisplayName: "My API Key",
		KeyString:   "AIzaSySecretKeyValue",
		Etag:        "etag-key",
		Annotations: annotationsJSON,
		Revision:    1,
		CreateTime:  now,
		UpdateTime:  updated,
		DeleteTime:  pgtype.Timestamptz{Valid: false},
	}

	proto := ApiKeyToProto(k, "meridian-broadcasting")

	assert.Equal(t, "organizations/meridian-broadcasting/keys/my-key", proto.Name)
	assert.Equal(t, "My API Key", proto.DisplayName)
	assert.Empty(t, proto.KeyString, "key_string should always be empty")
	assert.Equal(t, map[string]string{"created-by": "terraform"}, proto.Annotations)
}
