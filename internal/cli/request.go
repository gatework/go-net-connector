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
	requestPushPlan
	requestPushRun
)

// commandRequest is the validated action selected by the command tree. Parsed
// flags stay in options; this value owns execution intent and topology options.
// No later config load or presentation step can turn a plan into an apply.
type commandRequest struct {
	kind     requestKind
	topology workflow.TopologyOptions
}

func newCommandRequest(o options) (commandRequest, error) {
	if o.mode != "backup" && o.mode != "tftp" && o.mode != "topology" && o.mode != "push" {
		return commandRequest{}, usageError("用途无效，请查看 --for 选项")
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
	case "export":
		if o.mode != "backup" {
			return request, usageError("此用途不支持导出")
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
		case "push/plan":
			request.kind = requestPushPlan
		case "push/run":
			request.kind = requestPushRun
		}
		if o.mode == "topology" {
			request.topology = workflow.TopologyOptions{Limit: o.limit}
		}
	}
	if request.kind == requestInvalid {
		return request, usageError("此用途不支持所选操作")
	}
	return request, nil
}

func (r commandRequest) startsProgress() bool {
	switch r.kind {
	case requestDefaults, requestInitialize, requestShow, requestCheck, requestExport:
		return false
	default:
		return true
	}
}

func (r commandRequest) inventoryPreview() bool {
	return r.kind == requestInventory || r.kind == requestBackupPlan || r.kind == requestTFTPPlan
}

func (r commandRequest) needsRunConfirmation() bool {
	return r.kind == requestBackupRun || r.kind == requestTFTPRun || r.kind == requestTopologyApply || r.kind == requestPushRun
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
