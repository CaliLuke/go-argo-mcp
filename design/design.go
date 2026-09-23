package design

import (
	. "github.com/CaliLuke/loom-mcp/v2/dsl"
	. "github.com/CaliLuke/loom/dsl"
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
	Required("namespace", "workflow", "container", "total_lines", "matching_lines", "returned_lines", "logs")
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

	MCP("go-argo-mcp", "0.1.0")

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
			Required("workflow_name")
		})
		Result(WorkflowLogsResult)
		Tool("get_workflow_logs", "Get the latest matching log entries from a workflow's pods")
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
			Attribute("offset", Int, "Zero-based offset", func() { Default(0); Minimum(0) })
			Attribute("limit", Int, "Maximum nodes; defaults to 50", func() { Default(50); Minimum(1); Maximum(200) })
			Required("name")
		})
		Result(WorkflowNodesResult)
		Tool("get_workflow_nodes", "Get a filtered, bounded page of workflow nodes")
	})

	Method("GetWorkflowEvents", func() {
		Description("Observe workflow-related Kubernetes events during a bounded live window; this is not historical event listing.")
		readAnnotations()
		Payload(func() {
			Attribute("namespace", String, "Kubernetes namespace; defaults to ARGO_NAMESPACE")
			Attribute("name", String, "Exact workflow name")
			Attribute("limit", Int, "Maximum observed events; defaults to 50", func() { Default(50); Minimum(1); Maximum(200) })
			Attribute("duration_seconds", Int, "Observation duration in seconds; defaults to 2", func() { Default(2); Minimum(1); Maximum(10) })
			Required("name")
		})
		Result(WorkflowEventsResult)
		Tool("get_workflow_events", "Observe events for one workflow during a bounded live window")
	})

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
		Description("Get bounded artifact metadata and safe Argo download links; binary retrieval is deferred.")
		readAnnotations()
		Payload(func() {
			Attribute("namespace", String, "Kubernetes namespace; defaults to ARGO_NAMESPACE")
			Attribute("name", String, "Exact workflow name")
			Attribute("node_id", String, "Optional exact node ID")
			Attribute("offset", Int, "Zero-based offset", func() { Default(0); Minimum(0) })
			Attribute("limit", Int, "Maximum artifacts; defaults to 50", func() { Default(50); Minimum(1); Maximum(200) })
			Required("name")
		})
		Result(WorkflowArtifactsResult)
		Tool("get_workflow_artifacts", "Get artifact metadata and trusted Argo download links")
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
