package ui

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/fileio"
	"github.com/biptec/aws-ssm-params/internal/filter"
	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/randomx"
	"github.com/biptec/aws-ssm-params/internal/ssm"
)

type uiBackend interface {
	loadStatuses(context.Context, inventory.Items, filter.Groups, []string, bool, LoadProgress, StatusBatch) Statuses
	listRegions(context.Context) ([]string, error)
	readFile(string) ([]byte, error)
	writeFile(string, []byte, fs.FileMode) error
	statFile(string) (fs.FileInfo, error)
	randomValue(string, string) (string, error)
	saveParameter(context.Context, inventory.Item, string, string, ssm.ParameterType, ssm.PutParameterOptions, string, bool) statusUpdatedMsg
	deleteParameters(context.Context, inventory.Items, string, bool) deleteDoneMsg
}

type fileStore interface {
	readFile(string) ([]byte, error)
	writeFile(string, []byte, fs.FileMode) error
	statFile(string) (fs.FileInfo, error)
}

type localFileStore struct{}

func (localFileStore) readFile(path string) ([]byte, error) {
	data, err := fileio.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w", err)
	}
	return data, nil
}

func (localFileStore) writeFile(path string, data []byte, mode fs.FileMode) error {
	if err := fileio.WriteFile(path, data, mode); err != nil {
		return fmt.Errorf("%w", err)
	}
	return nil
}

func (localFileStore) statFile(path string) (fs.FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("%w", err)
	}
	return info, nil
}

type defaultBackend struct {
	client ssm.Client
	files  fileStore
}

func newDefaultBackend(client ssm.Client) defaultBackend {
	return defaultBackend{client: client, files: localFileStore{}}
}

func (backend defaultBackend) fileStore() fileStore {
	if backend.files == nil {
		return localFileStore{}
	}
	return backend.files
}

func (backend defaultBackend) loadStatuses(ctx context.Context, items inventory.Items, groups filter.Groups, regions []string, includeValues bool, progress LoadProgress, batch StatusBatch) Statuses {
	if len(groups) > 0 && len(items) == 0 {
		return LoadFilteredStatusesBatchForRegions(ctx, backend.client, groups, includeValues, regions, progress)
	}
	statuses := LoadStatusesBatchForRegionsStream(ctx, backend.client, items, includeValues, regions, progress, batch)
	return statuses.Filter(groups)
}

func (backend defaultBackend) listRegions(ctx context.Context) ([]string, error) {
	regions, err := backend.client.ListRegions(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w", err)
	}
	return regions, nil
}

func (backend defaultBackend) readFile(path string) ([]byte, error) {
	return backend.fileStore().readFile(path)
}

func (backend defaultBackend) writeFile(path string, data []byte, mode fs.FileMode) error {
	return backend.fileStore().writeFile(path, data, mode)
}

func (backend defaultBackend) statFile(path string) (fs.FileInfo, error) {
	return backend.fileStore().statFile(path)
}

func (defaultBackend) randomValue(kind, customLength string) (string, error) {
	switch kind {
	case "base64-32":
		value, err := randomx.Base64(32)
		return value, errors.Wrap(err, "generate base64 random value")
	case "hex-32":
		value, err := randomx.Hex(32)
		return value, errors.Wrap(err, "generate hex random value")
	case "uuid":
		value, err := randomx.UUID()
		return value, errors.Wrap(err, "generate UUID random value")
	case "base64-custom":
		n, err := strconv.Atoi(strings.TrimSpace(customLength))
		if err != nil || n <= 0 {
			return "", errors.New("invalid byte length")
		}
		value, err := randomx.Base64(n)
		return value, errors.Wrap(err, "generate custom base64 random value")
	default:
		return "", errors.New("unknown random value generator")
	}
}

func (backend defaultBackend) saveParameter(ctx context.Context, item inventory.Item, oldPath, value string, parameterType ssm.ParameterType, opts ssm.PutParameterOptions, pathsFile string, allowNamesFileUpdate bool) statusUpdatedMsg {
	if item.Region == "*" {
		return statusUpdatedMsg{path: item.Path, oldPath: oldPath, err: fmt.Errorf("cannot save %s without a concrete AWS region", item.Path)}
	}
	regionalClient := backend.client.ForRegion(item.Region)
	if err := regionalClient.PutParameterWithOptions(ctx, item.Path, value, parameterType, opts); err != nil {
		return statusUpdatedMsg{path: item.Path, oldPath: oldPath, err: err}
	}
	appendedToNamesFile := false
	if pathsFile != "" && allowNamesFileUpdate {
		appended, err := (inventory.PathsFile{Path: pathsFile}).Append(item.Path)
		if err != nil {
			st := LoadStatuses(ctx, regionalClient, inventory.Items{item}, true)[0]
			return statusUpdatedMsg{path: item.Path, oldPath: oldPath, status: st, message: "Updated " + item.Path, warning: fmt.Sprintf("Could not append %s to %s: %v", item.Path, pathsFile, err)}
		}
		if appended {
			appendedToNamesFile = true
			item.Kind = "path-file"
			item.Source = pathsFile
		}
	}
	st := LoadStatuses(ctx, regionalClient, inventory.Items{item}, true)[0]
	message := "Updated " + item.Path
	if appendedToNamesFile {
		message += " and added it to " + pathsFile
	}
	return statusUpdatedMsg{path: item.Path, oldPath: oldPath, status: st, message: message}
}

func (backend defaultBackend) deleteParameters(ctx context.Context, items inventory.Items, pathsFile string, allowNamesFileUpdate bool) deleteDoneMsg {
	byRegion := map[string][]string{}
	for _, item := range items {
		if item.Region == "*" {
			continue
		}
		byRegion[item.Region] = append(byRegion[item.Region], item.Path)
	}
	for region, paths := range byRegion {
		if err := backend.client.ForRegion(region).DeleteMany(ctx, paths); err != nil {
			return deleteDoneMsg{items: items, err: err}
		}
	}

	removeRows := pathsFile == ""
	if pathsFile != "" && allowNamesFileUpdate {
		if _, err := (inventory.PathsFile{Path: pathsFile}).Remove(items.Paths()); err != nil {
			return deleteDoneMsg{items: items, warning: fmt.Sprintf("Could not update %s after delete: %v", pathsFile, err)}
		}
		removeRows = true
	}
	return deleteDoneMsg{items: items, removeRows: removeRows}
}

func backendFor(m model) uiBackend {
	if m.backend != nil {
		return m.backend
	}
	return newDefaultBackend(m.client)
}
