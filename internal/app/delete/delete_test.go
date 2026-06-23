package deleter

import (
	"bytes"
	"context"
	"io"
	"sort"
	"strings"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/biptec/aws-ssm-params/internal/app"
	"github.com/biptec/aws-ssm-params/internal/filter"
	"github.com/biptec/aws-ssm-params/internal/prompt"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	ssmclient "github.com/biptec/aws-ssm-params/internal/ssm/client"
	"github.com/biptec/aws-ssm-params/internal/textio"
)

type fakeClientState struct {
	regions     []string
	deleteCalls []string
	deleteErr   error
}

type fakeClient struct {
	region string
	state  *fakeClientState
}

func (client *fakeClient) CheckAccess(context.Context) error { return nil }

func (client *fakeClient) ListRegions(context.Context) ([]string, error) {
	return append([]string(nil), client.state.regions...), nil
}

func (client *fakeClient) ForRegion(region string) ssmclient.Client {
	return &fakeClient{region: region, state: client.state}
}

func (client *fakeClient) DefaultRegion() string { return client.region }

func (client *fakeClient) GetMany(context.Context, []string) (parameters map[string]ssm.Parameter, errs map[string]error) {
	return map[string]ssm.Parameter{}, map[string]error{}
}

func (client *fakeClient) DescribeMany(context.Context, []string) map[string]ssm.Metadata {
	return map[string]ssm.Metadata{}
}

func (client *fakeClient) DescribeManyStrict(context.Context, []string) (metadataByPath map[string]ssm.Metadata, errorsByPath map[string]error) {
	return map[string]ssm.Metadata{}, map[string]error{}
}

func (client *fakeClient) ListParameterMetadata(context.Context) ([]ssm.Metadata, error) {
	return nil, nil
}

func (client *fakeClient) ListParameterMetadataWithFilters(context.Context, []ssm.ParameterFilter) ([]ssm.Metadata, error) {
	return nil, nil
}

func (client *fakeClient) PutParameterWithOptions(context.Context, string, string, ssm.ParameterType, ssm.PutParameterOptions) error {
	return nil
}

func (client *fakeClient) Delete(ctx context.Context, req *ssmclient.DeleteRequest) error {
	if err := ctx.Err(); err != nil {
		return errors.Wrap(err, "delete fake parameters")
	}

	pathsByRegion := map[string][]string{}
	seen := map[string]bool{}

	for _, parameter := range req.Parameters {
		name := strings.TrimSpace(parameter.Name)
		region := strings.TrimSpace(parameter.Region)

		if name == "" {
			continue
		}

		key := region + "\x00" + name
		if seen[key] {
			continue
		}

		seen[key] = true

		pathsByRegion[region] = append(pathsByRegion[region], name)
	}

	regions := make([]string, 0, len(pathsByRegion))
	for region := range pathsByRegion {
		regions = append(regions, region)
	}

	sort.Strings(regions)

	for _, region := range regions {
		client.state.deleteCalls = append(
			client.state.deleteCalls,
			region+":"+strings.Join(pathsByRegion[region], ","),
		)
	}

	return client.state.deleteErr
}

func testDependencies(state *fakeClientState, terminal *prompt.Terminal) dependencies {
	return dependencies{
		newClient: func(config ssmclient.Config) ssmclient.Client {
			return &fakeClient{region: config.Region, state: state}
		},
		openTerminal: func() (*prompt.Terminal, error) {
			if terminal == nil {
				return nil, errors.New("unexpected terminal request")
			}

			return terminal, nil
		},
	}
}

func TestDeleteNoConfirmExpandsNameListAcrossRegions(t *testing.T) {
	state := &fakeClientState{}
	options := &Options{
		Options: &app.Options{
			Region:  "eu-central-1",
			Regions: []string{"eu-central-1", "eu-north-1"},
		},
		Format:    textio.FormatJSON,
		NoConfirm: true,
	}

	var output bytes.Buffer

	err := runWithDependencies(
		context.Background(),
		options,
		strings.NewReader(`["/app/one","/app/two"]`),
		&output,
		testDependencies(state, nil),
	)

	require.NoError(t, err)
	assert.Equal(t, []string{
		"eu-central-1:/app/one,/app/two",
		"eu-north-1:/app/one,/app/two",
	}, state.deleteCalls)
	assert.Contains(t, output.String(), "Найдено 4 параметров.")
	assert.Contains(t, output.String(), "Удалено 4 параметров.")
}

func TestDeleteFiltersOnlyInputRecords(t *testing.T) {
	groups, err := filter.ParseGroups([]string{"name:/base/one;region:eu-north-1"})
	require.NoError(t, err)

	state := &fakeClientState{}
	options := &Options{
		Options: &app.Options{
			Region:       "eu-north-1",
			Regions:      []string{"eu-north-1", "eu-central-1"},
			FilterGroups: groups,
		},
		Format:   textio.FormatYAML,
		BasePath: "/base",
		DryRun:   true,
	}

	var output bytes.Buffer

	err = runWithDependencies(
		context.Background(),
		options,
		strings.NewReader("- one\n- two\n"),
		&output,
		testDependencies(state, nil),
	)

	require.NoError(t, err)
	assert.Empty(t, state.deleteCalls)
	assert.Contains(t, output.String(), "Найдено 1 параметров.")
	assert.Contains(t, output.String(), "DRY-RUN: удалить параметр /base/one из eu-north-1.")
	assert.NotContains(t, output.String(), "/base/two")
}

func TestDeleteReadsMappedKeyedRecords(t *testing.T) {
	state := &fakeClientState{}
	options := &Options{
		Options:  &app.Options{},
		Format:   textio.FormatJSON,
		KeyField: textio.FieldName,
		FieldMappings: textio.FieldMappings{
			{AWSName: textio.FieldRegion, FileName: "area"},
		},
		NoConfirm: true,
	}

	err := runWithDependencies(
		context.Background(),
		options,
		strings.NewReader(`{"/app/one":{"area":"eu-north-1"}}`),
		io.Discard,
		testDependencies(state, nil),
	)

	require.NoError(t, err)
	assert.Equal(t, []string{"eu-north-1:/app/one"}, state.deleteCalls)
}

func TestDeleteInteractiveSupportsDetailsSkipAndCancel(t *testing.T) {
	state := &fakeClientState{}
	options := &Options{
		Options: &app.Options{},
		Format:  textio.FormatJSON,
	}

	var terminalOutput bytes.Buffer

	terminal := prompt.New(strings.NewReader("d\ny\ns\nc\n"), &terminalOutput)

	var output bytes.Buffer

	err := runWithDependencies(
		context.Background(),
		options,
		strings.NewReader(`[
			{"name":"/app/one","region":"eu-north-1","description":"first","value":"top-secret"},
			{"name":"/app/two","region":"eu-north-1"},
			{"name":"/app/three","region":"eu-north-1"}
		]`),
		&output,
		testDependencies(state, terminal),
	)

	require.NoError(t, err)
	assert.Equal(t, []string{"eu-north-1:/app/one"}, state.deleteCalls)
	assert.Contains(t, terminalOutput.String(), "description: first")
	assert.Contains(t, terminalOutput.String(), "value: [скрыто]")
	assert.NotContains(t, terminalOutput.String(), "top-secret")
	assert.Contains(t, output.String(), "Найдено 3 параметров.")
	assert.Contains(t, output.String(), "Удаление отменено. Удалено: 1, пропущено: 1.")
}

func TestDeleteWithNoFilterMatchesPrintsZeroAndDoesNotPrompt(t *testing.T) {
	groups, err := filter.ParseGroups([]string{"name:/other/**"})
	require.NoError(t, err)

	state := &fakeClientState{}
	options := &Options{
		Options: &app.Options{
			Region:       "eu-north-1",
			Regions:      []string{"eu-north-1"},
			FilterGroups: groups,
		},
		Format: textio.FormatDotenv,
	}

	var output bytes.Buffer

	err = runWithDependencies(
		context.Background(),
		options,
		strings.NewReader("/app/one\n"),
		&output,
		testDependencies(state, nil),
	)

	require.NoError(t, err)
	assert.Equal(t, "Найдено 0 параметров.\n", output.String())
	assert.Empty(t, state.deleteCalls)
}

func TestDeleteAllRegionsExpandsRecordsWithoutRegion(t *testing.T) {
	state := &fakeClientState{regions: []string{"eu-north-1", "us-east-1"}}
	options := &Options{
		Options: &app.Options{AllRegions: true},
		Format:  textio.FormatDotenv,
		DryRun:  true,
	}

	var output bytes.Buffer

	err := runWithDependencies(
		context.Background(),
		options,
		strings.NewReader("/app/one\n"),
		&output,
		testDependencies(state, nil),
	)

	require.NoError(t, err)
	assert.Contains(t, output.String(), "/app/one из eu-north-1")
	assert.Contains(t, output.String(), "/app/one из us-east-1")
	assert.Empty(t, state.deleteCalls)
}

func TestDeleteRejectsEmptyInput(t *testing.T) {
	state := &fakeClientState{}
	options := &Options{
		Options: &app.Options{Region: "eu-north-1"},
		Format:  textio.FormatDotenv,
	}

	err := runWithDependencies(
		context.Background(),
		options,
		strings.NewReader(""),
		io.Discard,
		testDependencies(state, nil),
	)

	require.Error(t, err)
	assert.ErrorContains(t, err, "contains no parameters")
}
