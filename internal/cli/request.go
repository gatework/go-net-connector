package cli

import "github.com/gatework/go-net-connector/internal/workflow"

type requestKind uint8

const (
	requestInvalid requestKind = iota
	requestDefaults
	requestInitialize
	requestShow
	requestCheck
	requestExport
	requestInventory
	requestBackupPlan
	requestBackupRun
	requestTFTPPlan
	requestTFTPRun
	requestTopologyPlan
	requestTopologyDiscover
	requestTopologyApply
	requestDeployPlan
	requestDeployRun
	requestExecPlan
	requestExecRun
	requestRemotePlan
	requestRemoteRun
	requestDiff
)

// commandRequest is the validated action selected by the command tree. Parsed
// flags stay in options; this value owns execution intent and topology options.
// No later config load or presentation step can turn a plan into an apply.
type commandRequest struct {
	kind     requestKind
	topology workflow.TopologyOptions
	// useCase is the business operation of a pipeline command; nil for local
	// configuration commands.
	useCase useCase
}

func newCommandRequest(o options) (commandRequest, error) {
	if o.mode != "backup" && o.mode != "tftp" && o.mode != "topology" && o.mode != "deploy" && o.mode != "exec" && o.mode != "remote" {
		return commandRequest{}, newUsageError("用途无效，请查看 --for 选项")
	}
	request := commandRequest{}
	switch o.action {
	case "defaults":
		request.kind = requestDefaults
	case "init":
		request.kind = requestInitialize
	case "show":
		request.kind = requestShow
	case "check":
		request.kind = requestCheck
	case "diff":
		request.kind = requestDiff
	case "export":
		if o.mode != "backup" {
			return request, newUsageError("此用途不支持导出")
		}
		request.kind = requestExport
	case "inventory":
		request.kind = requestInventory
		request.topology = workflow.TopologyOptions{InventoryOnly: true, Limit: o.limit}
	case "plan", "run", "discover", "apply":
		switch o.mode + "/" + o.action {
		case "backup/plan":
			request.kind = requestBackupPlan
		case "backup/run":
			request.kind = requestBackupRun
		case "tftp/plan":
			request.kind = requestTFTPPlan
		case "tftp/run":
			request.kind = requestTFTPRun
		case "topology/plan":
			request.kind = requestTopologyPlan
		case "topology/discover":
			request.kind = requestTopologyDiscover
		case "topology/apply":
			request.kind = requestTopologyApply
		case "deploy/plan":
			request.kind = requestDeployPlan
		case "deploy/run":
			request.kind = requestDeployRun
		case "remote/plan":
			request.kind = requestRemotePlan
		case "remote/run":
			request.kind = requestRemoteRun
		case "exec/plan":
			request.kind = requestExecPlan
		case "exec/run":
			request.kind = requestExecRun
		}
		if o.mode == "topology" {
			request.topology = workflow.TopologyOptions{Limit: o.limit}
		}
	}
	if request.kind == requestInvalid {
		return request, newUsageError("此用途不支持所选操作")
	}
	request.useCase = request.newUseCase()
	return request, nil
}

// newUseCase maps a pipeline command to its business operation.
func (r commandRequest) newUseCase() useCase {
	switch r.kind {
	case requestInventory:
		return inventoryUseCase{}
	case requestBackupPlan, requestBackupRun:
		return backupUseCase{preview: r.kind == requestBackupPlan}
	case requestTFTPPlan, requestTFTPRun:
		return tftpUseCase{preview: r.kind == requestTFTPPlan}
	case requestDeployPlan, requestDeployRun:
		return &deployUseCase{preview: r.kind == requestDeployPlan}
	case requestRemotePlan, requestRemoteRun:
		return remoteUseCase{preview: r.kind == requestRemotePlan}
	case requestExecPlan, requestExecRun:
		return &execUseCase{preview: r.kind == requestExecPlan}
	case requestTopologyPlan, requestTopologyDiscover, requestTopologyApply:
		return topologyUseCase{mode: r.topologyMode(), options: r.topology}
	default: // configuration requests and invalid input have no pipeline use case
		return nil
	}
}

func (r commandRequest) startsProgress() bool {
	switch r.kind {
	case requestDefaults, requestInitialize, requestShow, requestCheck, requestExport, requestDiff:
		return false
	default:
		return true
	}
}

func (r commandRequest) needsRunConfirmation() bool {
	return r.kind == requestBackupRun || r.kind == requestTFTPRun || r.kind == requestRemoteRun || r.kind == requestTopologyApply || r.kind == requestDeployRun
}

func (r commandRequest) topologyMode() string {
	switch r.kind {
	case requestInventory, requestTopologyPlan:
		return "plan"
	case requestTopologyDiscover:
		return "discover"
	case requestTopologyApply:
		return "apply"
	default:
		return ""
	}
}
