package design

import (
	. "github.com/CaliLuke/loom-mcp/v2/dsl"
	. "github.com/CaliLuke/loom/dsl"

	"github.com/CaliLuke/go-argo-mcp/internal/version"
)

var WorkflowSummary = Type("WorkflowSummary", func() {
	Description("Workflow summary returned by Argo.")
	Attribute("name", String, "Workflow name")
	Attribute("namespace", String, "Kubernetes namespace")
	Attribute("status", String, "Workflow phase or status")
	Attribute("progress", String, "Completed nodes over total nodes")
	Attribute("started_at", String, "RFC3339 start timestamp")
	Attribute("finished_at", String, "RFC3339 finish timestamp")
	Attribute("duration", String, "Elapsed workflow duration")
	Required("name", "namespace", "status")
})

var WorkflowDetailResult = Type("WorkflowDetailResult", func() {
	Attribute("name", String, "Workflow name")
	Attribute("namespace", String, "Kubernetes namespace")
	Attribute("status", String, "Current workflow phase")
	Attribute("progress", String, "Completed nodes over total nodes")
	Attribute("started_at", String, "RFC3339 start timestamp")
	Attribute("finished_at", String, "RFC3339 finish timestamp")
	Attribute("duration", String, "Elapsed workflow duration")
	Attribute("message", String, "Status message reported by Argo")
	Attribute("labels", MapOf(String, String), "Workflow labels")
	Attribute("annotations", MapOf(String, String), "Workflow annotations")
	Attribute("parameters", MapOf(String, String), "Workflow input parameters")
	Attribute("outputs", MapOf(String, String), "Workflow output parameters")
	Required("name", "namespace", "status")
})

var WorkflowLogsResult = Type("WorkflowLogsResult", func() {
	Attribute("namespace", String, "Kubernetes namespace")
	Attribute("workflow", String, "Workflow name")
	Attribute("pod", String, "Pod filter, when provided")
	Attribute("container", String, "Container name")
	Attribute("total_lines", Int, "Log entries returned by Argo before local filtering")
	Attribute("matching_lines", Int, "Entries matching the search term")
	Attribute("returned_lines", Int, "Entries included in logs")
	Attribute("search_term", String, "Case-insensitive local search term")
	Attribute("max_lines", Int, "Maximum matching entries returned; omitted when unlimited")
	Attribute("note", String, "Explanation of empty or truncated results")
	Attribute("logs", String, "Rendered log entries; empty when no entries match")
	Attribute("source", String, "Log source: live or archive")
	Attribute("truncated", Boolean, "Whether collection was bounded before all available content")
	Required("namespace", "workflow", "container", "total_lines", "matching_lines", "returned_lines", "logs", "source", "truncated")
})

var ListWorkflowsResult = Type("ListWorkflowsResult", func() {
	Attribute("workflows", ArrayOf(WorkflowSummary), "Matching workflows")
	Attribute("count", Int, "Number of workflows returned")
	Attribute("namespace", String, "Kubernetes namespace queried")
	Attribute("status", String, "Applied workflow status filter")
	Attribute("source", String, "Data source; always argo for live results")
	Attribute("continue", String, "Opaque continuation token for the next page; replay it with the same filters and limit")
	Attribute("has_more", Boolean, "Whether another page is available")
	Required("workflows", "count", "source", "has_more")
})

var ActionResult = Type("ActionResult", func() {
	Attribute("status", String, "Outcome of the requested action", func() { Enum("ok", "dry_run", "denied") })
	Attribute("message", String, "Human-readable outcome")
	Attribute("namespace", String, "Kubernetes namespace")
	Attribute("name", String, "Target resource name")
	Attribute("reason", String, "Termination reason")
	Attribute("preview", String, "Exact destructive action that would be performed")
	Attribute("instructions", String, "Required next step when the action did not run")
	Attribute("confirmation_token", String, "Single-use token scoped to the previewed destructive action")
	Attribute("restart_successful", Boolean, "Whether successful workflow nodes were also restarted")
	Required("status", "message")
})

var CronWorkflowSummary = Type("CronWorkflowSummary", func() {
	Attribute("name", String, "CronWorkflow name")
	Attribute("namespace", String, "Kubernetes namespace")
	Attribute("schedule", String, "Legacy single schedule, when configured")
	Attribute("schedules", ArrayOf(String), "All configured CronWorkflow schedules")
	Attribute("timezone", String, "IANA timezone for the schedules")
	Attribute("suspended", Boolean, "Whether Argo scheduling is suspended")
	Required("name", "namespace")
})

var ListCronWorkflowsResult = Type("ListCronWorkflowsResult", func() {
	Attribute("cron_workflows", ArrayOf(CronWorkflowSummary), "Matching CronWorkflows")
	Attribute("count", Int, "Number of CronWorkflows returned")
	Attribute("namespace", String, "Kubernetes namespace queried")
	Attribute("suspended", Boolean, "Applied suspension filter")
	Attribute("source", String, "Data source; always argo for live results")
	Attribute("continue", String, "Opaque continuation token for the next page; replay it with the same filters and limit")
	Attribute("has_more", Boolean, "Whether another page is available")
	Required("cron_workflows", "count", "source", "has_more")
})

var CronWorkflowDetailResult = Type("CronWorkflowDetailResult", func() {
	Attribute("name", String, "CronWorkflow name")
	Attribute("namespace", String, "Kubernetes namespace")
	Attribute("schedule", String, "Legacy single schedule, when configured")
	Attribute("schedules", ArrayOf(String), "All configured CronWorkflow schedules")
	Attribute("timezone", String, "IANA timezone for the schedules")
	Attribute("suspended", Boolean, "Whether Argo scheduling is suspended")
	Attribute("last_scheduled_time", String, "RFC3339 time Argo last scheduled a workflow")
	Attribute("next_scheduled_time", String, "Next nominal run from the configured schedules; conditions may prevent execution")
	Attribute("source", String, "Data source; always argo for live results")
	Required("name", "source")
})

var CronHistoryEntry = Type("CronHistoryEntry", func() {
	Attribute("name", String, "Generated workflow name")
	Attribute("status", String, "Workflow phase")
	Attribute("started_at", String, "RFC3339 start timestamp")
	Attribute("finished_at", String, "RFC3339 finish timestamp")
	Attribute("duration", String, "Elapsed workflow duration")
	Required("name")
})

var CronHistoryResult = Type("CronHistoryResult", func() {
	Attribute("name", String, "CronWorkflow name")
	Attribute("namespace", String, "Kubernetes namespace queried")
	Attribute("history", ArrayOf(CronHistoryEntry), "Recent workflows owned by this CronWorkflow")
	Attribute("count", Int, "Number of history entries returned")
	Attribute("source", String, "Data source; always argo for live results")
	Attribute("continue", String, "Opaque continuation token for the next page; replay it with the same limit")
	Attribute("has_more", Boolean, "Whether another page is available")
	Required("name", "history", "count", "source", "has_more")
})

var TemplateSummary = Type("TemplateSummary", func() {
	Attribute("name", String, "WorkflowTemplate name")
	Attribute("namespace", String, "Kubernetes namespace")
	Attribute("entrypoint", String, "Default template entrypoint")
	Required("name")
})

var ListWorkflowTemplatesResult = Type("ListWorkflowTemplatesResult", func() {
	Attribute("templates", ArrayOf(TemplateSummary), "Matching WorkflowTemplates")
	Attribute("count", Int, "Number of WorkflowTemplates returned")
	Attribute("namespace", String, "Kubernetes namespace queried")
	Attribute("label_selector", String, "Applied Kubernetes label selector")
	Attribute("source", String, "Data source; always argo for live results")
	Attribute("continue", String, "Opaque continuation token for the next page; replay it with the same filters and limit")
	Attribute("has_more", Boolean, "Whether another page is available")
	Required("templates", "count", "source", "has_more")
})

var WorkflowTemplateDetailResult = Type("WorkflowTemplateDetailResult", func() {
	Attribute("name", String, "WorkflowTemplate name")
	Attribute("namespace", String, "Kubernetes namespace")
	Attribute("entrypoint", String, "Default template entrypoint")
	Attribute("template_names", ArrayOf(String), "Template definitions available in this resource")
	Attribute("source", String, "Data source; always argo for live results")
	Required("name", "source")
})

var ClusterWorkflowTemplateSummary = Type("ClusterWorkflowTemplateSummary", func() {
	Attribute("name", String, "ClusterWorkflowTemplate name")
	Attribute("entrypoint", String, "Default template entrypoint")
	Required("name")
})

var ListClusterWorkflowTemplatesResult = Type("ListClusterWorkflowTemplatesResult", func() {
	Attribute("templates", ArrayOf(ClusterWorkflowTemplateSummary), "Matching ClusterWorkflowTemplates")
	Attribute("count", Int, "Number of ClusterWorkflowTemplates returned")
	Attribute("label_selector", String, "Applied Kubernetes label selector")
	Attribute("source", String, "Data source; always argo for live results")
	Attribute("continue", String, "Opaque continuation token for the next page; replay it with the same filters and limit")
	Attribute("has_more", Boolean, "Whether another page is available")
	Required("templates", "count", "source", "has_more")
})

var ClusterWorkflowTemplateDetailResult = Type("ClusterWorkflowTemplateDetailResult", func() {
	Attribute("name", String, "ClusterWorkflowTemplate name")
	Attribute("entrypoint", String, "Default template entrypoint")
	Attribute("template_names", ArrayOf(String), "Template definitions available in this resource")
	Attribute("source", String, "Data source; always argo for live results")
	Required("name", "source")
})

var WorkflowNodeSummary = Type("WorkflowNodeSummary", func() {
	Attribute("id", String, "Stable node ID")
	Attribute("name", String, "Node name")
	Attribute("display_name", String, "Human-readable node name")
	Attribute("type", String, "Node type")
	Attribute("phase", String, "Node phase")
	Attribute("template_name", String, "Template name")
	Attribute("boundary_id", String, "Template boundary node ID")
	Attribute("children", ArrayOf(String), "Child node IDs; links may point outside the current page")
	Attribute("started_at", String, "RFC3339 start timestamp")
	Attribute("finished_at", String, "RFC3339 finish timestamp")
	Attribute("message", String, "Diagnostic message, truncated to 4 KiB")
	Attribute("input_parameters", MapOf(String, String), "Node input parameters")
	Attribute("output_parameters", MapOf(String, String), "Node output parameters")
	Attribute("output_result", String, "Node output result, truncated to 16 KiB")
	Attribute("exit_code", String, "Node exit code")
	Required("id", "name", "type", "children")
})

var WorkflowNodesResult = Type("WorkflowNodesResult", func() {
	Attribute("nodes", ArrayOf(WorkflowNodeSummary), "Filtered nodes sorted by stable ID")
	Attribute("total", Int, "Total filtered nodes before paging")
	Attribute("count", Int, "Nodes returned")
	Attribute("next_offset", Int, "Offset for the next page; absent when exhausted")
	Attribute("truncated", Boolean, "Whether another page exists or fields were shortened")
	Attribute("fields_truncated", Boolean, "Whether any displayed field or child list was shortened")
	Attribute("note", String, "Truncation or paging note")
	Required("nodes", "total", "count", "truncated", "fields_truncated")
})

var WorkflowEventSummary = Type("WorkflowEventSummary", func() {
	Attribute("type", String, "Event type")
	Attribute("reason", String, "Event reason")
	Attribute("message", String, "Diagnostic message, truncated to 4 KiB")
	Attribute("count", Int, "Occurrence count")
	Attribute("first_timestamp", String, "First observation timestamp")
	Attribute("last_timestamp", String, "Last observation timestamp")
	Attribute("event_time", String, "Event timestamp")
	Required("type", "count")
})

var WorkflowEventsResult = Type("WorkflowEventsResult", func() {
	Attribute("events", ArrayOf(WorkflowEventSummary), "Events observed during the bounded watch window")
	Attribute("count", Int, "Events returned")
	Attribute("limit_reached", Boolean, "Whether collection stopped at the requested limit")
	Attribute("fields_truncated", Boolean, "Whether diagnostic text was shortened")
	Attribute("note", String, "Observation-window explanation")
	Required("events", "count", "limit_reached", "fields_truncated", "note")
})

var ArchivedWorkflowSummary = Type("ArchivedWorkflowSummary", func() {
	Attribute("uid", String, "Archive UID")
	Attribute("name", String, "Workflow name")
	Attribute("namespace", String, "Kubernetes namespace")
	Attribute("status", String, "Workflow phase")
	Attribute("started_at", String, "RFC3339 start timestamp")
	Attribute("finished_at", String, "RFC3339 finish timestamp")
	Required("uid", "name", "namespace", "status")
})

var ListArchivedWorkflowsResult = Type("ListArchivedWorkflowsResult", func() {
	Attribute("workflows", ArrayOf(ArchivedWorkflowSummary), "Archived workflow summaries")
	Attribute("count", Int, "Items returned")
	Attribute("continue", String, "Opaque continuation token")
	Attribute("has_more", Boolean, "Whether another page is available")
	Required("workflows", "count", "has_more")
})

var ArchivedWorkflowDetailResult = Type("ArchivedWorkflowDetailResult", func() {
	Attribute("uid", String, "Archive UID")
	Attribute("name", String, "Workflow name")
	Attribute("namespace", String, "Kubernetes namespace")
	Attribute("status", String, "Workflow phase")
	Attribute("started_at", String, "RFC3339 start timestamp")
	Attribute("finished_at", String, "RFC3339 finish timestamp")
	Attribute("message", String, "Diagnostic message")
	Attribute("labels", MapOf(String, String), "Selected labels")
	Attribute("annotations", MapOf(String, String), "Selected annotations")
	Attribute("parameters", MapOf(String, String), "Selected input parameters")
	Attribute("outputs", MapOf(String, String), "Selected output parameters")
	Attribute("truncated", Boolean, "Whether fields were shortened or entries omitted")
	Required("uid", "name", "namespace", "status", "labels", "annotations", "parameters", "outputs", "truncated")
})

var WorkflowArtifactSummary = Type("WorkflowArtifactSummary", func() {
	Attribute("name", String, "Artifact name")
	Attribute("node_id", String, "Owning node ID")
	Attribute("direction", String, "inputs or outputs", func() { Enum("inputs", "outputs") })
	Attribute("path", String, "Container artifact path")
	Attribute("optional", Boolean, "Whether the artifact is optional")
	Attribute("download_url", String, "Safe Argo artifact download URL")
	Required("name", "node_id", "direction", "optional")
})

var WorkflowArtifactsResult = Type("WorkflowArtifactsResult", func() {
	Attribute("artifacts", ArrayOf(WorkflowArtifactSummary), "Artifact metadata sorted by node, direction, and name")
	Attribute("total", Int, "Total filtered artifacts before paging")
	Attribute("count", Int, "Artifacts returned")
	Attribute("next_offset", Int, "Offset for the next page; absent when exhausted")
	Attribute("truncated", Boolean, "Whether another page exists or fields/links were shortened")
	Attribute("fields_truncated", Boolean, "Whether displayed fields were shortened or a link was omitted")
	Attribute("note", String, "Truncation or paging note")
	Required("artifacts", "total", "count", "truncated", "fields_truncated")
})

var ArtifactContentResult = Type("ArtifactContentResult", func() {
	Attribute("namespace", String, "Kubernetes namespace")
	Attribute("name", String, "Workflow name")
	Attribute("node_id", String, "Owning node ID")
	Attribute("artifact_name", String, "Artifact name")
	Attribute("direction", String, "Artifact direction", func() { Enum("inputs", "outputs") })
	Attribute("text", String, "UTF-8 artifact text")
	Attribute("offset_bytes", Int, "Requested byte offset")
	Attribute("returned_bytes", Int, "Original artifact bytes returned")
	Attribute("next_offset", Int, "Next byte offset; present only when more bytes remain")
	Attribute("has_more", Boolean, "Whether more artifact bytes remain")
	Attribute("source", String, "Data source; always argo")
	Attribute("archive_uid", String, "Archive UID when reading retained workflow data")
	Attribute("note", String, "Pagination or retention guidance")
	Required("namespace", "name", "node_id", "artifact_name", "direction", "text", "offset_bytes", "returned_bytes", "has_more", "source")
})

var ResourceSpecResult = Type("ResourceSpecResult", func() {
	Attribute("kind", String, "Resource kind")
	Attribute("name", String, "Resource name")
	Attribute("namespace", String, "Kubernetes namespace; absent for cluster-scoped resources")
	Attribute("section", String, "Selected spec section")
	Attribute("template_name", String, "Selected template name")
	Attribute("spec_json", String, "Canonical JSON for the selected upstream fields")
	Attribute("source", String, "Data source; always argo")
	Required("kind", "name", "section", "spec_json", "source")
})

var PodCondition = Type("PodCondition", func() {
	Attribute("type", String, "Condition type")
	Attribute("status", String, "Condition status")
	Attribute("reason", String, "Condition reason")
	Attribute("message", String, "Condition message")
	Required("type", "status", "reason", "message")
})

var ContainerState = Type("ContainerState", func() {
	Attribute("status", String, "waiting, running, terminated, or unknown")
	Attribute("reason", String, "State reason")
	Attribute("message", String, "State message")
	Attribute("exit_code", Int, "Termination exit code")
	Attribute("started_at", String, "RFC3339 start timestamp")
	Attribute("finished_at", String, "RFC3339 finish timestamp")
	Required("status", "reason", "message")
})

var ContainerDiagnostic = Type("ContainerDiagnostic", func() {
	Attribute("name", String, "Container name")
	Attribute("restart_count", Int, "Container restart count")
	Attribute("ready", Boolean, "Whether the container is ready")
	Attribute("state", ContainerState, "Current container state")
	Attribute("last_state", ContainerState, "Previous container state")
	Required("name", "restart_count", "ready", "state", "last_state")
})

var PodDiagnosticEvent = Type("PodDiagnosticEvent", func() {
	Attribute("type", String, "Event type")
	Attribute("reason", String, "Event reason")
	Attribute("message", String, "Event message")
	Attribute("count", Int, "Occurrence count")
	Attribute("first_timestamp", String, "First observation timestamp")
	Attribute("last_timestamp", String, "Last observation timestamp")
	Required("type", "reason", "message", "count", "first_timestamp", "last_timestamp")
})

var WorkflowPodDiagnostic = Type("WorkflowPodDiagnostic", func() {
	Attribute("name", String, "Pod name")
	Attribute("uid", String, "Pod UID")
	Attribute("phase", String, "Pod phase")
	Attribute("node_name", String, "Assigned Kubernetes node")
	Attribute("conditions", ArrayOf(PodCondition), "Bounded pod conditions")
	Attribute("init_containers", ArrayOf(ContainerDiagnostic), "Bounded init-container states")
	Attribute("containers", ArrayOf(ContainerDiagnostic), "Bounded container states")
	Attribute("events", ArrayOf(PodDiagnosticEvent), "UID-scoped Kubernetes events")
	Attribute("events_truncated", Boolean, "Whether more events exist")
	Attribute("truncated", Boolean, "Whether any pod field was shortened")
	Attribute("events_error", String, "Safe per-pod event retrieval error")
	Required("name", "uid", "phase", "node_name", "conditions", "init_containers", "containers", "events", "events_truncated", "truncated")
})

var WorkflowPodDiagnosticsResult = Type("WorkflowPodDiagnosticsResult", func() {
	Attribute("namespace", String, "Kubernetes namespace")
	Attribute("name", String, "Workflow name")
	Attribute("pods", ArrayOf(WorkflowPodDiagnostic), "Workflow-owned pods")
	Attribute("count", Int, "Pods returned")
	Attribute("continue", String, "Opaque continuation token")
	Attribute("has_more", Boolean, "Whether another Kubernetes page exists")
	Attribute("truncated", Boolean, "Whether output fields were shortened")
	Attribute("source", String, "Data source; always kubernetes")
	Required("namespace", "name", "pods", "count", "has_more", "truncated", "source")
})

var WaitWorkflowResult = Type("WaitWorkflowResult", func() {
	Attribute("workflow", WorkflowDetailResult, "Latest workflow detail")
	Attribute("completed", Boolean, "Whether the workflow reached a terminal phase")
	Attribute("timed_out", Boolean, "Whether the owned wait deadline expired")
	Attribute("source", String, "Data source; always argo")
	Required("workflow", "completed", "timed_out", "source")
})

var ServerContextResult = Type("ServerContextResult", func() {
	Attribute("build_version", String, "CLI build version")
	Attribute("mcp_version", String, "Declared MCP protocol version")
	Attribute("transport", String, "Active MCP transport")
	Attribute("default_namespace", String, "Configured default namespace")
	Attribute("allowed_namespaces", ArrayOf(String), "Configured namespace allow list")
	Attribute("denied_namespaces", ArrayOf(String), "Configured namespace deny list")
	Attribute("allow_mutations", Boolean, "Whether mutations are enabled")
	Attribute("allow_destructive", Boolean, "Whether destructive mutations are enabled")
	Attribute("require_confirmation", Boolean, "Whether destructive confirmation is required")
	Attribute("argo_configured", Boolean, "Whether Argo is configured")
	Attribute("kubernetes_configured", Boolean, "Whether Kubernetes diagnostics are configured")
	Attribute("argo_status", String, "Argo context status", func() { Enum("unconfigured", "available", "unavailable") })
	Attribute("argo_version", String, "Upstream Argo version")
	Attribute("note", String, "Namespace policy and upstream-status context")
	Required("build_version", "mcp_version", "transport", "default_namespace", "allowed_namespaces", "denied_namespaces", "allow_mutations", "allow_destructive", "require_confirmation", "argo_configured", "kubernetes_configured", "argo_status", "note")
})

var LintResult = Type("LintResult", func() {
	Attribute("valid", Boolean, "Whether Argo accepted the manifest")
	Attribute("name", String, "Manifest resource name")
	Attribute("namespace", String, "Resolved namespace for namespaced resources")
	Attribute("scope", String, "namespaced or cluster")
	Attribute("source", String, "Validation source; always argo")
	Required("valid", "name", "scope", "source")
})

var CreatedWorkflowResult = Type("CreatedWorkflowResult", func() {
	Attribute("status", String, "Outcome; always ok after successful dispatch")
	Attribute("message", String, "Human-readable outcome")
	Attribute("workflow", WorkflowSummary, "Argo-created workflow summary")
	Required("status", "message", "workflow")
})

var readAnnotations = func() {
	Meta("mcp:annotation:readOnlyHint", "true")
	Meta("mcp:annotation:destructiveHint", "false")
}

var mutationAnnotations = func() {
	Meta("mcp:annotation:readOnlyHint", "false")
	Meta("mcp:annotation:destructiveHint", "false")
	Meta("mcp:annotation:idempotentHint", "false")
}

var destructiveAnnotations = func() {
	Meta("mcp:annotation:readOnlyHint", "false")
	Meta("mcp:annotation:destructiveHint", "true")
}

var namespacedWorkflowReadPayload = func() {
	Attribute("namespace", String, "Kubernetes namespace; defaults to ARGO_NAMESPACE")
	Attribute("name", String, "Exact workflow name")
}

var namespacedWorkflowReadMethod = func(methodName, description string, payloadExtras func(), result any, toolName, toolDescription string) {
	Method(methodName, func() {
		Description(description)
		readAnnotations()
		Payload(func() {
			namespacedWorkflowReadPayload()
			payloadExtras()
			Required("name")
		})
		Result(result)
		Tool(toolName, toolDescription)
	})
}

var _ = API("go-argo-mcp", func() {
	Title("Go Argo MCP")
	Description("Lightweight Go MCP server for operating Argo Workflows with environment-only configuration.")
})

var _ = Service("argo", func() {
	Description("MCP service for Argo Workflows operations.")

	Error("configuration_error", func() {
		Remedy(func() {
			RemedyCode("argo.configure")
			SafeMessage("Argo API is not configured.")
			RetryHint("Set ARGO_BASE_URL and authentication environment variables, then retry.")
		})
	})
	Error("argo_api_error", func() {
		Temporary()
		Remedy(func() {
			RemedyCode("argo.api.retry")
			SafeMessage("The Argo API request failed.")
			RetryHint("Verify Argo connectivity and credentials, then retry.")
		})
	})
	Error("argo_not_found", func() {
		Remedy(func() {
			RemedyCode("argo.resource.not_found")
			SafeMessage("The requested Argo resource was not found.")
			RetryHint("Check the resource name and namespace, then retry.")
		})
	})
	Error("argo_access_denied", func() {
		Remedy(func() {
			RemedyCode("argo.access.denied")
			SafeMessage("Argo denied access to the requested resource.")
			RetryHint("Check Argo credentials and RBAC permissions.")
		})
	})
	Error("argo_request_rejected", func() {
		Remedy(func() {
			RemedyCode("argo.request.rejected")
			SafeMessage("Argo rejected the request.")
			RetryHint("Check the tool inputs and current resource state.")
		})
	})
	Error("invalid_input", func() {
		Remedy(func() {
			RemedyCode("argo.input.invalid")
			SafeMessage("The tool input is invalid.")
			RetryHint("Correct the named input and retry. No Argo request was made for local validation failures.")
		})
	})
	Error("invalid_state", func() {
		Remedy(func() {
			RemedyCode("argo.state.invalid")
			SafeMessage("The Argo resource is not in a state that permits this action.")
			RetryHint("Inspect the current workflow state before deciding whether to retry.")
		})
	})
	Error("namespace_denied", func() {
		Remedy(func() {
			RemedyCode("argo.namespace.denied")
			SafeMessage("Access to this namespace is denied.")
			RetryHint("Use an allowed namespace or update the namespace policy.")
		})
	})
	Error("confirmation_invalid", func() {
		Remedy(func() {
			RemedyCode("argo.confirmation.refresh")
			SafeMessage("The confirmation token is missing, expired, already used, or scoped to another action.")
			RetryHint("Run the destructive tool in dry-run mode to obtain a fresh token, then retry once.")
		})
	})
	Error("kubernetes_configuration_error", func() {
		Remedy(func() {
			RemedyCode("kubernetes.configure")
			SafeMessage("Kubernetes diagnostics are not configured.")
			RetryHint("Set KUBERNETES_API_URL and Kubernetes credentials, then retry.")
		})
	})
	Error("kubernetes_access_denied", func() {
		Remedy(func() {
			RemedyCode("kubernetes.access.denied")
			SafeMessage("Kubernetes denied access to the requested resource.")
			RetryHint("Check the Kubernetes token and pods/events RBAC permissions.")
		})
	})
	Error("kubernetes_not_found", func() {
		Remedy(func() {
			RemedyCode("kubernetes.resource.not_found")
			SafeMessage("The requested Kubernetes resource was not found.")
			RetryHint("Rediscover the current pod and retry.")
		})
	})
	Error("kubernetes_api_error", func() {
		Temporary()
		Remedy(func() {
			RemedyCode("kubernetes.api.retry")
			SafeMessage("The Kubernetes API request failed.")
			RetryHint("Check Kubernetes connectivity and retry.")
		})
	})
	Error("kubernetes_response_error", func() {
		Remedy(func() {
			RemedyCode("kubernetes.response.invalid")
			SafeMessage("Kubernetes returned an invalid or unsupported response.")
			RetryHint("Check the inputs and Kubernetes server compatibility.")
		})
	})

	MCP("go-argo-mcp", version.MCPVersion)

	Method("ListWorkflows", func() {
		Description("List workflows in a namespace with an optional status filter.")
		readAnnotations()
		Payload(func() {
			Attribute("namespace", String, "Kubernetes namespace to query; defaults to the server's ARGO_NAMESPACE")
			Attribute("status", String, "Optional workflow status filter", func() {
				Enum("Running", "Succeeded", "Failed", "Pending", "Error")
			})
			Attribute("limit", Int, "Maximum number of workflows to return; defaults to 50 when omitted", func() {
				Default(50)
				Minimum(1)
				Maximum(200)
			})
			Attribute("continue", String, "Opaque continuation token returned by a previous call; replay with the same filters and limit")
		})
		Result(ListWorkflowsResult)
		Tool("list_workflows", "List workflows in one Kubernetes namespace, optionally filtered by phase")
	})

	Method("GetWorkflow", func() {
		Description("Get detailed information about a specific workflow.")
		readAnnotations()
		Payload(func() {
			Attribute("namespace", String, "Kubernetes namespace; defaults to the server's ARGO_NAMESPACE")
			Attribute("name", String, "Exact workflow name; use list_workflows to discover names")
			Required("name")
		})
		Result(WorkflowDetailResult)
		Tool("get_workflow", "Get status, timing, metadata, parameters, and outputs for one workflow")
	})

	Method("GetWorkflowLogs", func() {
		Description("Get workflow logs, optionally filtered by pod and a case-insensitive search term.")
		readAnnotations()
		Payload(func() {
			Attribute("namespace", String, "Kubernetes namespace; defaults to the server's ARGO_NAMESPACE")
			Attribute("workflow_name", String, "Exact workflow name; use list_workflows to discover names")
			Attribute("pod_name", String, "Optional exact pod name; omit to collect logs across workflow pods")
			Attribute("container", String, "Container name; defaults to main", func() { Default("main") })
			Attribute("search", String, "Optional case-insensitive search across log text and pod names")
			Attribute("max_lines", Int, "Maximum lines to return; zero returns all lines", func() {
				Default(200)
				Minimum(0)
			})
			Attribute("source", String, "Log source", func() { Default("auto"); Enum("auto", "live", "archive") })
			Attribute("node_id", String, "Exact archived node ID")
			Attribute("archive_uid", String, "Archive UID")
			Attribute("max_bytes", Int, "Maximum collected bytes; defaults to 1 MiB", func() { Default(1048576); Minimum(1024); Maximum(4194304) })
			Required("workflow_name")
		})
		Result(WorkflowLogsResult)
		Tool("get_workflow_logs", "Get bounded matching live or retained log entries for a workflow")
	})

	Method("TerminateWorkflow", func() {
		Description("Preview or terminate a running workflow when destructive operations are enabled.")
		destructiveAnnotations()
		Payload(func() {
			Attribute("namespace", String, "Kubernetes namespace; defaults to the server's ARGO_NAMESPACE")
			Attribute("name", String, "Exact workflow name")
			Attribute("reason", String, "Operator-visible reason for termination and part of confirmation scope")
			Attribute("dry_run", Boolean, "Preview mode; defaults to true and does not call Argo")
			Attribute("confirmation_token", String, "Single-use token returned by a matching dry-run preview")
			Required("name", "reason")
		})
		Result(ActionResult)
		Tool("terminate_workflow", "Preview or terminate a workflow; requires MCP_ALLOW_MUTATIONS and MCP_ALLOW_DESTRUCTIVE and may require confirmation")
	})

	Method("RetryWorkflow", func() {
		Description("Preview or retry a workflow when mutation and destructive operations are enabled.")
		destructiveAnnotations()
		Payload(func() {
			Attribute("namespace", String, "Kubernetes namespace; defaults to the server's ARGO_NAMESPACE")
			Attribute("name", String, "Exact workflow name")
			Attribute("restart_successful", Boolean, "Also restart successful steps; defaults to false")
			Attribute("dry_run", Boolean, "Preview mode; defaults to true and does not call Argo")
			Attribute("confirmation_token", String, "Single-use token returned by a matching dry-run preview")
			Required("name")
		})
		Result(ActionResult)
		Tool("retry_workflow", "Preview or retry a workflow; requires MCP_ALLOW_MUTATIONS and MCP_ALLOW_DESTRUCTIVE")
	})

	Method("ListCronWorkflows", func() {
		Description("List CronWorkflows in one namespace.")
		readAnnotations()
		Payload(func() {
			Attribute("namespace", String, "Kubernetes namespace; defaults to the server's ARGO_NAMESPACE")
			Attribute("suspended", Boolean, "Optional suspension-state filter")
			Attribute("limit", Int, "Maximum number of CronWorkflows to return; defaults to 50 when omitted", func() {
				Default(50)
				Minimum(1)
				Maximum(200)
			})
			Attribute("continue", String, "Opaque continuation token returned by a previous call; replay with the same filters and limit")
		})
		Result(ListCronWorkflowsResult)
		Tool("list_cron_workflows", "List CronWorkflows in one Kubernetes namespace")
	})

	Method("GetCronWorkflow", func() {
		Description("Get CronWorkflow schedule configuration and scheduling timestamps.")
		readAnnotations()
		Payload(func() {
			Attribute("namespace", String, "Kubernetes namespace; defaults to the server's ARGO_NAMESPACE")
			Attribute("name", String, "Exact CronWorkflow name; use list_cron_workflows to discover names")
			Required("name")
		})
		Result(CronWorkflowDetailResult)
		Tool("get_cron_workflow", "Get CronWorkflow schedules, timezone, suspension state, and last and next scheduled times")
	})

	Method("GetCronHistory", func() {
		Description("Get recent workflows created by an existing CronWorkflow.")
		readAnnotations()
		Payload(func() {
			Attribute("namespace", String, "Kubernetes namespace; defaults to the server's ARGO_NAMESPACE")
			Attribute("name", String, "Exact CronWorkflow name; use list_cron_workflows to discover names")
			Attribute("limit", Int, "Maximum history entries to return; defaults to 10 when omitted", func() {
				Default(10)
				Minimum(1)
				Maximum(200)
			})
			Attribute("continue", String, "Opaque continuation token returned by a previous call; replay with the same limit")
			Required("name")
		})
		Result(CronHistoryResult)
		Tool("get_cron_history", "Get recent workflows created by an existing CronWorkflow")
	})

	Method("ToggleCronSuspension", func() {
		Description("Suspend or resume a CronWorkflow when mutation operations are enabled.")
		mutationAnnotations()
		Payload(func() {
			Attribute("namespace", String, "Kubernetes namespace; defaults to the server's ARGO_NAMESPACE")
			Attribute("name", String, "Exact CronWorkflow name")
			Attribute("suspend", Boolean, "True to suspend scheduling; false to resume scheduling")
			Required("name", "suspend")
		})
		Result(ActionResult)
		Tool("toggle_cron_suspension", "Suspend or resume a CronWorkflow; requires MCP_ALLOW_MUTATIONS")
	})

	Method("ListWorkflowTemplates", func() {
		Description("List WorkflowTemplates in one namespace.")
		readAnnotations()
		Payload(func() {
			Attribute("namespace", String, "Kubernetes namespace; defaults to the server's ARGO_NAMESPACE")
			Attribute("label_selector", String, "Optional Kubernetes label selector")
			Attribute("limit", Int, "Maximum number of WorkflowTemplates to return; defaults to 50 when omitted", func() {
				Default(50)
				Minimum(1)
				Maximum(200)
			})
			Attribute("continue", String, "Opaque continuation token returned by a previous call; replay with the same filters and limit")
		})
		Result(ListWorkflowTemplatesResult)
		Tool("list_workflow_templates", "List WorkflowTemplates in one Kubernetes namespace")
	})

	Method("GetWorkflowTemplate", func() {
		Description("Get one WorkflowTemplate's entrypoint and template names.")
		readAnnotations()
		Payload(func() {
			Attribute("namespace", String, "Kubernetes namespace; defaults to the server's ARGO_NAMESPACE")
			Attribute("name", String, "Exact WorkflowTemplate name; use list_workflow_templates to discover names")
			Required("name")
		})
		Result(WorkflowTemplateDetailResult)
		Tool("get_workflow_template", "Get one WorkflowTemplate's entrypoint and template names")
	})

	Method("ListClusterWorkflowTemplates", func() {
		Description("List ClusterWorkflowTemplates (cluster-scoped).")
		readAnnotations()
		Payload(func() {
			Attribute("label_selector", String, "Optional Kubernetes label selector")
			Attribute("limit", Int, "Maximum number of ClusterWorkflowTemplates to return; defaults to 50 when omitted", func() {
				Default(50)
				Minimum(1)
				Maximum(200)
			})
			Attribute("continue", String, "Opaque continuation token returned by a previous call; replay with the same filters and limit")
		})
		Result(ListClusterWorkflowTemplatesResult)
		Tool("list_cluster_workflow_templates", "List ClusterWorkflowTemplates (cluster-scoped)")
	})

	Method("GetClusterWorkflowTemplate", func() {
		Description("Get one ClusterWorkflowTemplate's entrypoint and template names.")
		readAnnotations()
		Payload(func() {
			Attribute("name", String, "Exact ClusterWorkflowTemplate name; use list_cluster_workflow_templates to discover names")
			Required("name")
		})
		Result(ClusterWorkflowTemplateDetailResult)
		Tool("get_cluster_workflow_template", "Get one ClusterWorkflowTemplate's entrypoint and template names")
	})

	Method("GetWorkflowNodes", func() {
		Description("Get a bounded page of nodes from one hydrated workflow.")
		readAnnotations()
		Payload(func() {
			Attribute("namespace", String, "Kubernetes namespace; defaults to ARGO_NAMESPACE")
			Attribute("name", String, "Exact workflow name")
			Attribute("phase", String, "Optional exact node phase")
			Attribute("node_id", String, "Optional exact node ID")
			Attribute("archive_uid", String, "Archive UID; omit for live workflow data")
			Attribute("offset", Int, "Zero-based offset", func() { Default(0); Minimum(0) })
			Attribute("limit", Int, "Maximum nodes; defaults to 50", func() { Default(50); Minimum(1); Maximum(200) })
			Required("name")
		})
		Result(WorkflowNodesResult)
		Tool("get_workflow_nodes", "Get a filtered, bounded page of workflow nodes")
	})

	namespacedWorkflowReadMethod(
		"GetWorkflowEvents",
		"Observe workflow-related Kubernetes events during a bounded live window; this is not historical event listing.",
		func() {
			Attribute("limit", Int, "Maximum observed events; defaults to 50", func() { Default(50); Minimum(1); Maximum(200) })
			Attribute("duration_seconds", Int, "Observation duration in seconds; defaults to 2", func() { Default(2); Minimum(1); Maximum(10) })
		},
		WorkflowEventsResult,
		"get_workflow_events",
		"Observe events for one workflow during a bounded live window",
	)

	Method("ListArchivedWorkflows", func() {
		Description("List archived workflows in one authorized namespace.")
		readAnnotations()
		Payload(func() {
			Attribute("namespace", String, "Kubernetes namespace; defaults to ARGO_NAMESPACE")
			Attribute("label_selector", String, "Optional Kubernetes label selector")
			Attribute("name_prefix", String, "Optional workflow name prefix")
			Attribute("limit", Int, "Maximum archived workflows; defaults to 50", func() { Default(50); Minimum(1); Maximum(200) })
			Attribute("continue", String, "Opaque continuation token")
		})
		Result(ListArchivedWorkflowsResult)
		Tool("list_archived_workflows", "List archived workflows in one namespace")
	})

	Method("GetArchivedWorkflow", func() {
		Description("Get compact details for one archived workflow in an authorized namespace.")
		readAnnotations()
		Payload(func() {
			Attribute("namespace", String, "Kubernetes namespace; defaults to ARGO_NAMESPACE")
			Attribute("uid", String, "Archive UID")
			Required("uid")
		})
		Result(ArchivedWorkflowDetailResult)
		Tool("get_archived_workflow", "Get one archived workflow by UID")
	})

	Method("GetWorkflowArtifacts", func() {
		Description("Get bounded artifact metadata and safe Argo download links; use read_workflow_artifact for bounded text content.")
		readAnnotations()
		Payload(func() {
			Attribute("namespace", String, "Kubernetes namespace; defaults to ARGO_NAMESPACE")
			Attribute("name", String, "Exact workflow name")
			Attribute("node_id", String, "Optional exact node ID")
			Attribute("archive_uid", String, "Archive UID; omit for live workflow data")
			Attribute("offset", Int, "Zero-based offset", func() { Default(0); Minimum(0) })
			Attribute("limit", Int, "Maximum artifacts; defaults to 50", func() { Default(50); Minimum(1); Maximum(200) })
			Required("name")
		})
		Result(WorkflowArtifactsResult)
		Tool("get_workflow_artifacts", "Get artifact metadata and trusted Argo download links")
	})

	Method("ReadWorkflowArtifact", func() {
		Description("Read a bounded UTF-8 chunk from an artifact declared by one workflow node.")
		readAnnotations()
		Payload(func() {
			Attribute("namespace", String, "Kubernetes namespace; defaults to ARGO_NAMESPACE")
			Attribute("name", String, "Exact workflow name")
			Attribute("node_id", String, "Exact workflow node ID")
			Attribute("artifact_name", String, "Exact declared artifact name")
			Attribute("direction", String, "Artifact direction", func() { Default("outputs"); Enum("inputs", "outputs") })
			Attribute("archive_uid", String, "Archive UID; omit for live workflow data")
			Attribute("offset_bytes", Int, "Byte offset", func() { Default(0); Minimum(0); Maximum(16777216) })
			Attribute("max_bytes", Int, "Maximum bytes to return", func() { Default(65536); Minimum(4); Maximum(262144) })
			Required("name", "node_id", "artifact_name")
		})
		Result(ArtifactContentResult)
		Tool("read_workflow_artifact", "Read bounded UTF-8 text from a declared workflow artifact")
	})

	Method("GetResourceSpec", func() {
		Description("Get a preserved JSON section from an Argo resource specification.")
		readAnnotations()
		Payload(func() {
			Attribute("kind", String, "Resource kind", func() { Enum("workflow", "workflow_template", "cluster_workflow_template", "cron_workflow") })
			Attribute("name", String, "Exact resource name")
			Attribute("namespace", String, "Kubernetes namespace; forbidden for cluster scope")
			Attribute("section", String, "Spec section", func() { Default("summary"); Enum("summary", "arguments", "templates", "spec") })
			Attribute("template_name", String, "Exact template name; valid only for templates")
			Attribute("max_bytes", Int, "Maximum serialized JSON bytes", func() { Default(65536); Minimum(1024); Maximum(262144) })
			Required("kind", "name")
		})
		Result(ResourceSpecResult)
		Tool("get_resource_spec", "Get a preserved bounded section of an Argo resource specification")
	})

	Method("GetWorkflowPodDiagnostics", func() {
		Description("Get bounded Kubernetes pod state and events for a live workflow.")
		readAnnotations()
		Payload(func() {
			Attribute("namespace", String, "Kubernetes namespace; defaults to ARGO_NAMESPACE")
			Attribute("name", String, "Exact workflow name")
			Attribute("pod_name", String, "Optional exact pod name")
			Attribute("limit", Int, "Maximum pods", func() { Default(20); Minimum(1); Maximum(50) })
			Attribute("continue", String, "Opaque Kubernetes continuation token")
			Required("name")
		})
		Result(WorkflowPodDiagnosticsResult)
		Tool("get_workflow_pod_diagnostics", "Get workflow-owned pod failures, container states, and events")
	})

	namespacedWorkflowReadMethod(
		"WaitWorkflow",
		"Poll one workflow until it completes, the bounded duration expires, or the caller cancels.",
		func() {
			Attribute("duration_seconds", Int, "Maximum wait duration", func() { Default(10); Minimum(1); Maximum(30) })
			Attribute("poll_interval_seconds", Int, "Polling interval", func() { Default(2); Minimum(1); Maximum(5) })
		},
		WaitWorkflowResult,
		"wait_workflow",
		"Wait briefly for a workflow to reach a terminal phase",
	)

	Method("GetServerContext", func() {
		Description("Get sanitized server identity, policy, backend configuration, and Argo availability.")
		readAnnotations()
		Payload(func() {})
		Result(ServerContextResult)
		Tool("get_server_context", "Get sanitized server and backend context")
	})

	Method("LintWorkflow", func() {
		Description("Validate a complete opaque Workflow manifest with Argo without creating it.")
		readAnnotations()
		Payload(func() {
			Attribute("namespace", String, "Kubernetes namespace; defaults to ARGO_NAMESPACE")
			Attribute("manifest_json", String, "Complete Workflow JSON object, maximum 256 KiB", func() { MaxLength(262144) })
			Required("manifest_json")
		})
		Result(LintResult)
		Tool("lint_workflow", "Validate a Workflow manifest with Argo")
	})

	Method("LintWorkflowTemplate", func() {
		Description("Validate a complete opaque namespaced or cluster WorkflowTemplate manifest with Argo.")
		readAnnotations()
		Payload(func() {
			Attribute("namespace", String, "Optional for namespaced templates and defaults to ARGO_NAMESPACE; forbidden for cluster scope")
			Attribute("cluster_scope", Boolean, "Validate a ClusterWorkflowTemplate; defaults to false", func() { Default(false) })
			Attribute("manifest_json", String, "Complete template JSON object, maximum 256 KiB", func() { MaxLength(262144) })
			Required("manifest_json")
		})
		Result(LintResult)
		Tool("lint_workflow_template", "Validate a WorkflowTemplate or ClusterWorkflowTemplate manifest with Argo")
	})

	Method("SubmitWorkflowTemplate", func() {
		Description("Submit a WorkflowTemplate or ClusterWorkflowTemplate as a new workflow.")
		mutationAnnotations()
		Payload(func() {
			Attribute("namespace", String, "Kubernetes namespace; defaults to ARGO_NAMESPACE")
			Attribute("template_name", String, "Source template name")
			Attribute("cluster_scope", Boolean, "Submit a ClusterWorkflowTemplate; defaults to false", func() { Default(false) })
			Attribute("parameters", MapOf(String, String), "Parameter overrides")
			Required("template_name")
		})
		Result(CreatedWorkflowResult)
		Tool("submit_workflow_template", "Create a workflow from a template; requires MCP_ALLOW_MUTATIONS")
	})

	Method("SuspendWorkflow", func() {
		Description("Suspend a workflow.")
		mutationAnnotations()
		Payload(func() {
			Attribute("namespace", String, "Kubernetes namespace; defaults to ARGO_NAMESPACE")
			Attribute("name", String, "Exact workflow name")
			Required("name")
		})
		Result(ActionResult)
		Tool("suspend_workflow", "Suspend a workflow; requires MCP_ALLOW_MUTATIONS")
	})

	Method("ResumeWorkflow", func() {
		Description("Resume a whole suspended workflow.")
		mutationAnnotations()
		Payload(func() {
			Attribute("namespace", String, "Kubernetes namespace; defaults to ARGO_NAMESPACE")
			Attribute("name", String, "Exact workflow name")
			Required("name")
		})
		Result(ActionResult)
		Tool("resume_workflow", "Resume a workflow; requires MCP_ALLOW_MUTATIONS")
	})

	Method("ResubmitWorkflow", func() {
		Description("Resubmit a workflow as a new workflow.")
		mutationAnnotations()
		Payload(func() {
			Attribute("namespace", String, "Kubernetes namespace; defaults to ARGO_NAMESPACE")
			Attribute("name", String, "Source workflow name")
			Attribute("memoized", Boolean, "Reuse successful outputs; defaults to false", func() { Default(false) })
			Attribute("parameters", MapOf(String, String), "Parameter overrides")
			Required("name")
		})
		Result(CreatedWorkflowResult)
		Tool("resubmit_workflow", "Resubmit a workflow; requires MCP_ALLOW_MUTATIONS")
	})

	Method("TriggerCronWorkflow", func() {
		Description("Trigger a CronWorkflow immediately as a new workflow.")
		mutationAnnotations()
		Payload(func() {
			Attribute("namespace", String, "Kubernetes namespace; defaults to ARGO_NAMESPACE")
			Attribute("name", String, "CronWorkflow name")
			Attribute("parameters", MapOf(String, String), "Parameter overrides")
			Required("name")
		})
		Result(CreatedWorkflowResult)
		Tool("trigger_cron_workflow", "Create a workflow from a CronWorkflow; requires MCP_ALLOW_MUTATIONS")
	})
})
