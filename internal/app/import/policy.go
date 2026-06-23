package importer

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/prompt"
)

// PolicyAction controls whether a create or update operation proceeds.
type PolicyAction string

const (
	// PolicyNone writes without an additional policy decision.
	PolicyNone PolicyAction = ""
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
	terminal, err := prompt.Open()
	if err != nil {
		return false, errors.Wrap(err, "open write confirmation terminal")
	}
	defer func() { _ = terminal.Close() }()

	questionAction := "Create"
	if action == writeOperationUpdate {
		questionAction = "Update"
	}

	var question string

	if region != "" {
		question = fmt.Sprintf("%s parameter %s in %s? [y/N] ", questionAction, name, region)
	} else {
		question = fmt.Sprintf("%s parameter %s? [y/N] ", questionAction, name)
	}

	answer, err := terminal.ReadLine(question)
	if err != nil {
		return false, errors.Wrap(err, "confirm parameter write")
	}

	answer = strings.ToLower(strings.TrimSpace(answer))

	return answer == "y" || answer == "yes", nil
}

func (action PolicyAction) resolve(operation writeOperation, region, name string) (bool, error) {
	switch action {
	case PolicyNone:
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

func (action PolicyAction) resolveDryRun(operation writeOperation, name string) (bool, error) {
	switch action {
	case PolicyNone, PolicyAsk:
		return true, nil
	case PolicySkip:
		return false, nil
	case PolicyError:
		return false, fmt.Errorf("parameter %s would %s; write policy stops the operation", name, operation)
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
