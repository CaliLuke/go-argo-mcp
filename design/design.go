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
		Meta("mcp:annotation:readOnlyHint", "true")
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
		Meta("mcp:annotation:readOnlyHint", "true")
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
		Meta("mcp:annotation:readOnlyHint", "true")
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
		Meta("mcp:annotation:destructiveHint", "true")
		Payload(func() {
			Attribute("namespace", String, "Kubernetes namespace; defaults to the server's ARGO_NAMESPACE")
			Attribute("name", String, "Exact workflow name")
			Attribute("reason", String, "Operator-visible reason for termination and part of confirmation scope")
			Attribute("dry_run", Boolean, "Preview mode; defaults to true and does not call Argo")
			Attribute("confirmation_token", String, "Single-use token returned by a matching dry-run preview")
			Required("name", "reason")
		})
		Result(ActionResult)
		Tool("terminate_workflow", "Preview or terminate a workflow; requires MCP_ALLOW_DESTRUCTIVE and may require confirmation")
	})

	Method("RetryWorkflow", func() {
		Description("Retry a workflow when mutation operations are enabled.")
		Meta("mcp:annotation:destructiveHint", "true")
		Payload(func() {
			Attribute("namespace", String, "Kubernetes namespace; defaults to the server's ARGO_NAMESPACE")
			Attribute("name", String, "Exact workflow name")
			Attribute("restart_successful", Boolean, "Also restart successful steps; defaults to false")
			Required("name")
		})
		Result(ActionResult)
		Tool("retry_workflow", "Retry a workflow; requires MCP_ALLOW_MUTATIONS")
	})

	Method("ListCronWorkflows", func() {
		Description("List CronWorkflows in one namespace.")
		Meta("mcp:annotation:readOnlyHint", "true")
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
		Meta("mcp:annotation:readOnlyHint", "true")
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
		Meta("mcp:annotation:readOnlyHint", "true")
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
		Meta("mcp:annotation:destructiveHint", "true")
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
		Meta("mcp:annotation:readOnlyHint", "true")
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
		Meta("mcp:annotation:readOnlyHint", "true")
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
		Meta("mcp:annotation:readOnlyHint", "true")
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
		Meta("mcp:annotation:readOnlyHint", "true")
		Payload(func() {
			Attribute("name", String, "Exact ClusterWorkflowTemplate name; use list_cluster_workflow_templates to discover names")
			Required("name")
		})
		Result(ClusterWorkflowTemplateDetailResult)
		Tool("get_cluster_workflow_template", "Get one ClusterWorkflowTemplate's entrypoint and template names")
	})
})
