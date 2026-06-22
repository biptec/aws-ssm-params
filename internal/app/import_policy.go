package app

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	crerr "github.com/cockroachdb/errors"
)

type writePolicyAction string

const (
	writePolicyDefault writePolicyAction = ""
	writePolicySkip    writePolicyAction = "skip"
	writePolicyError   writePolicyAction = "error"
	writePolicyAsk     writePolicyAction = "ask"
)

type writePolicy struct {
	OnCreate writePolicyAction
	OnUpdate writePolicyAction
}

type writeOperation string

const (
	writeOperationCreate writeOperation = "create"
	writeOperationUpdate writeOperation = "update"
)

func parseWritePolicy(ctx *CLIContext) (writePolicy, error) {
	onCreate, err := parseWritePolicyAction(ctx.String("on-create"), "on-create")
	if err != nil {
		return writePolicy{}, err
	}
	onUpdate, err := parseWritePolicyAction(ctx.String("on-update"), "on-update")
	if err != nil {
		return writePolicy{}, err
	}
	return writePolicy{OnCreate: onCreate, OnUpdate: onUpdate}, nil
}

func parseWritePolicyAction(value, flagName string) (writePolicyAction, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return writePolicyDefault, nil
	case "skip":
		return writePolicySkip, nil
	case "error":
		return writePolicyError, nil
	case "ask":
		return writePolicyAsk, nil
	default:
		return "", fmt.Errorf("unsupported --%s value %q; use skip, error, or ask", flagName, value)
	}
}

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
		return false, crerr.Wrap(err, "read write confirmation")
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}

func resolveWritePolicy(action writePolicyAction, operation writeOperation, region, name string) (bool, error) {
	switch action {
	case writePolicyDefault:
		return true, nil
	case writePolicySkip:
		return false, nil
	case writePolicyError:
		return false, fmt.Errorf("parameter %s would %s; --on-%s=error stops the command", name, operation, operationPolicyName(operation))
	case writePolicyAsk:
		return askWriteConfirmation(operation, region, name)
	default:
		return false, fmt.Errorf("unsupported write policy action %q", action)
	}
}

func operationPolicyName(operation writeOperation) string {
	if operation == writeOperationCreate {
		return "create"
	}
	return "update"
}

func logSkipped(logger *slog.Logger, command string, operation writeOperation, policy writePolicyAction, region, name string) {
	if logger == nil {
		return
	}
	logger.Info(command+" record skipped", "action", string(operation), "policy", "on-"+operationPolicyName(operation)+"="+string(policy), "region", region, "name", name)
}

func logRecordError(logger *slog.Logger, command string, operation writeOperation, region, name string, err error) {
	if logger == nil {
		return
	}
	logger.Error(command+" record failed", "action", string(operation), "region", region, "name", name, "error", err)
}

func logContinueOnError(logger *slog.Logger, command, region, name string) {
	if logger == nil {
		return
	}
	logger.Info("continuing after "+command+" error", "region", region, "name", name)
}
