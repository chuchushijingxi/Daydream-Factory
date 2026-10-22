package httpapi

import (
	"encoding/json"
	"reflect"
	"testing"

	"drama-agent/internal/application/ports"
)

func TestProjectSummaryViewUsesProtectedThumbnailsAndKeepsAnEmptyArray(t *testing.T) {
	for _, ids := range [][]string{nil, {"one"}, {"one", "two"}, {"one", "two", "three"}} {
		row := projectSummaryView(ports.ProjectSummary{ID: "project", Title: "当前项目", Revision: 7,
			SpaceID: "team", OwnerID: "responsible", ListMetadata: ports.ListMetadata{CreatorID: "creator", CreatorName: "来源创建人", SharedWithMe: true, EffectiveAccess: 1}, PreviewImageFileIDs: ids})
		want := []string{}
		for _, id := range ids {
			want = append(want, "/api/projects/project/files/"+id+"/thumbnail")
		}
		if !reflect.DeepEqual(row.PreviewImages, want) {
			t.Fatal(row.PreviewImages, want)
		}
		raw, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]json.RawMessage
		if err = json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		if string(body["spaceId"]) != `"team"` || string(body["ownerId"]) != `"responsible"` || string(body["creatorId"]) != `"creator"` || string(body["sharedWithMe"]) != "true" || string(body["effectiveAccess"]) != "1" {
			t.Fatal("space/source/access projection missing", string(raw))
		}
		if len(body) != 13 || string(body["previewImages"]) == "null" || body["previewImageFileIds"] != nil || body["assets"] != nil {
			t.Fatal("list exposed internal project data or null thumbnails", string(raw))
		}
	}
}
