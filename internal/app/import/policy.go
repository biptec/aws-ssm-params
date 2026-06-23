package importer

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/cockroachdb/errors"
)

// PolicyAction controls whether a create or update operation proceeds.
type PolicyAction string

const (
	// PolicyDefault writes without an additional policy decision.
	PolicyDefault PolicyAction = ""
	// PolicySkip skips the operation.
	PolicySkip PolicyAction = "skip"
	// PolicyError rejects the operation.
	PolicyError PolicyAction = "error"
	// PolicyAsk requests interactive confirmation.
	PolicyAsk PolicyAction = "ask"
)

// Policy configures create and update behavior for imported records.
type Policy struct {
	OnCreate PolicyAction
	OnUpdate PolicyAction
}

func (policy Policy) operation(exists bool) (writeOperation, PolicyAction) {
	if exists {
		return writeOperationUpdate, policy.OnUpdate
	}

	return writeOperationCreate, policy.OnCreate
}

type writeOperation string

const (
	writeOperationCreate writeOperation = "create"
	writeOperationUpdate writeOperation = "update"
)

func askWriteConfirmation(action writeOperation, region, name string) (bool, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false, errors.New("ask requires an interactive terminal")
	}
	defer func() { _ = tty.Close() }()

	questionAction := "Create"
	if action == writeOperationUpdate {
		questionAction = "Update"
	}

	if region != "" {
		_, _ = fmt.Fprintf(tty, "%s parameter %s in %s? [y/N] ", questionAction, name, region)
	} else {
		_, _ = fmt.Fprintf(tty, "%s parameter %s? [y/N] ", questionAction, name)
	}

	answer, err := bufio.NewReader(tty).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, errors.Wrap(err, "read write confirmation")
	}

	answer = strings.ToLower(strings.TrimSpace(answer))

	return answer == "y" || answer == "yes", nil
}

func (action PolicyAction) resolve(operation writeOperation, region, name string) (bool, error) {
	switch action {
	case PolicyDefault:
		return true, nil
	case PolicySkip:
		return false, nil
	case PolicyError:
		return false, fmt.Errorf("parameter %s would %s; write policy stops the operation", name, operation)
	case PolicyAsk:
		return askWriteConfirmation(operation, region, name)
	default:
		return false, fmt.Errorf("unsupported write policy action %q", action)
	}
}

func logSkipped(logger *slog.Logger, operation writeOperation, policy PolicyAction, region, name string) {
	if logger == nil {
		return
	}

	logger.Info("record skipped", "action", string(operation), "policy", string(policy), "region", region, "name", name)
}

func logRecordError(logger *slog.Logger, operation writeOperation, region, name string, err error) {
	if logger == nil {
		return
	}

	logger.Error("record failed", "action", string(operation), "region", region, "name", name, "error", err)
}

func logContinueOnError(logger *slog.Logger, region, name string) {
	if logger == nil {
		return
	}

	logger.Info("continuing after record error", "region", region, "name", name)
}
