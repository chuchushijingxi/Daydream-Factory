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
			PreviewImageFileIDs: ids})
		want := []string{}
		for _, id := range ids {
			want = append(want, "/api/files/"+id+"/thumbnail")
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
		if len(body) != 7 || string(body["previewImages"]) == "null" || body["previewImageFileIds"] != nil || body["assets"] != nil {
			t.Fatal("list exposed internal project data or null thumbnails", string(raw))
		}
	}
}
