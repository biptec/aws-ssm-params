package filter

import (
	"strings"
	"testing"
)

func TestExtglobPathSemantics(t *testing.T) {
	t.Parallel()

	matcher, err := Compile("/prod/*")
	if err != nil {
		t.Fatalf("compile pattern: %v", err)
	}
	if !matcher.Match("/prod/db") {
		t.Fatal("expected /prod/* to match one path segment")
	}
	if matcher.Match("/prod/app/db") {
		t.Fatal("expected /prod/* not to match multiple path segments")
	}

	recursive, err := Compile("/prod/**")
	if err != nil {
		t.Fatalf("compile recursive pattern: %v", err)
	}
	for _, value := range []string{"/prod/db", "/prod/app/db", "/prod/app/backend/token"} {
		if !recursive.Match(value) {
			t.Fatalf("expected /prod/** to match %s", value)
		}
	}
}

func TestExtglobAlternatives(t *testing.T) {
	t.Parallel()

	matcher, err := Compile("/prod/@(app|worker)/*")
	if err != nil {
		t.Fatalf("compile pattern: %v", err)
	}
	for _, value := range []string{"/prod/app/db", "/prod/worker/token"} {
		if !matcher.Match(value) {
			t.Fatalf("expected alternative pattern to match %s", value)
		}
	}
	if matcher.Match("/prod/web/db") {
		t.Fatal("expected alternative pattern not to match unlisted segment")
	}
}

func TestExtglobNegativeAlternatives(t *testing.T) {
	t.Parallel()

	matcher, err := Compile("/prod/!(old|tmp)/**")
	if err != nil {
		t.Fatalf("compile pattern: %v", err)
	}
	if !matcher.Match("/prod/app/db") {
		t.Fatal("expected negative pattern to match allowed segment")
	}
	for _, value := range []string{"/prod/old/db", "/prod/tmp/token"} {
		if matcher.Match(value) {
			t.Fatalf("expected negative pattern not to match %s", value)
		}
	}
}

func TestParseGroupsUsesOrOfAndGroups(t *testing.T) {
	t.Parallel()

	groups, err := ParseGroups([]string{
		"name:/prod/*;region:eu-north*",
		"name:/github/token;tier:advanced",
		"/app-infra/production/himins/sparkyfitness",
	})
	if err != nil {
		t.Fatalf("parse groups: %v", err)
	}
	matches := []Record{
		{Name: "/prod/db", Region: "eu-north-1"},
		{Name: "/github/token", Tier: "Advanced"},
		{Name: "/app-infra/production/himins/sparkyfitness"},
	}
	for _, record := range matches {
		if !groups.Match(record) {
			t.Fatalf("expected record to match: %+v", record)
		}
	}
	if groups.Match(Record{Name: "/prod/db", Region: "us-east-1"}) {
		t.Fatal("expected same-group conditions to use AND")
	}
}

func TestParseFileSupportsCommentsAndORLines(t *testing.T) {
	t.Parallel()

	groups, err := ParseFile(strings.NewReader(`
# production parameters
name:/prod/*;region:eu*;tier:advanced

/app-infra/production/himins/sparkyfitness # bare name shortcut
`))
	if err != nil {
		t.Fatalf("parse file: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	if !groups.Match(Record{Name: "/prod/db", Region: "eu-north-1", Tier: "Advanced"}) {
		t.Fatal("expected first filters-file line to match")
	}
	if !groups.Match(Record{Name: "/app-infra/production/himins/sparkyfitness"}) {
		t.Fatal("expected bare name filters-file line to match")
	}
}

func TestAWSFiltersAreSafePrefilters(t *testing.T) {
	t.Parallel()

	group, err := ParseGroup("name:/prod/@(app|worker)/*;tier:advanced;region:eu*")
	if err != nil {
		t.Fatalf("parse group: %v", err)
	}
	filters := group.AWSFilters()
	want := []AWSFilter{
		{Key: "Name", Option: "BeginsWith", Values: []string{"/prod/"}},
		{Key: "Tier", Option: "Equals", Values: []string{"Advanced"}},
	}
	if len(filters) != len(want) {
		t.Fatalf("expected %d AWS filters, got %d: %#v", len(want), len(filters), filters)
	}
	for i := range want {
		if filters[i].Key != want[i].Key || filters[i].Option != want[i].Option || strings.Join(filters[i].Values, ",") != strings.Join(want[i].Values, ",") {
			t.Fatalf("unexpected AWS filter at %d: got %#v want %#v", i, filters[i], want[i])
		}
	}
}

func TestGroupExactNameRecognizesLiteralNameGroups(t *testing.T) {
	t.Parallel()

	group, err := ParseGroup("/app/prod/token;type:SecureString")
	if err != nil {
		t.Fatalf("parse group: %v", err)
	}

	name, ok := group.ExactName()

	if !ok {
		t.Fatal("expected exact name")
	}
	if name != "/app/prod/token" {
		t.Fatalf("unexpected exact name: %s", name)
	}
}

func TestGroupExactNameRejectsWildcardNameGroups(t *testing.T) {
	t.Parallel()

	group, err := ParseGroup("/app/prod/*;type:SecureString")
	if err != nil {
		t.Fatalf("parse group: %v", err)
	}

	_, ok := group.ExactName()

	if ok {
		t.Fatal("did not expect wildcard name to be exact")
	}
}
