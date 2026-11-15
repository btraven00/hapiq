package biostudies

import (
	"encoding/json"
	"testing"
)

// E-MTAB-14004 groups its links in a nested array; older studies list them flat.
func TestLinksAcceptNestedArrays(t *testing.T) {
	const doc = `{"links":[
		{"url":"SAMEA1","attributes":[{"name":"Type","value":"BioSample"}]},
		[{"url":"ERP159314","attributes":[{"name":"Type","value":"ENA"}]},{"url":"ERP2"}]
	]}`
	var s Section
	if err := json.Unmarshal([]byte(doc), &s); err != nil {
		t.Fatal(err)
	}
	if len(s.Links) != 3 || s.Links[1].URL != "ERP159314" || s.Links[1].LinkType() != "ENA" {
		t.Errorf("links = %+v", s.Links)
	}
}
