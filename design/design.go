package design

import (
	. "github.com/CaliLuke/loom-mcp/dsl"
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
	Attribute("name", String)
	Attribute("namespace", String)
	Attribute("status", String)
	Attribute("progress", String)
	Attribute("started_at", String)
	Attribute("finished_at", String)
	Attribute("duration", String)
	Attribute("message", String)
	Attribute("labels", MapOf(String, String))
	Attribute("annotations", MapOf(String, String))
	Attribute("parameters", MapOf(String, String))
	Attribute("outputs", MapOf(String, String))
	Required("name", "namespace", "status")
})

var WorkflowLogsResult = Type("WorkflowLogsResult", func() {
	Attribute("namespace", String)
	Attribute("workflow", String)
	Attribute("pod", String)
	Attribute("container", String)
	Attribute("total_lines", Int)
	Attribute("matching_lines", Int)
	Attribute("returned_lines", Int)
	Attribute("search_term", String)
	Attribute("max_lines", Int)
	Attribute("note", String)
	Attribute("logs", String)
	Required("namespace", "workflow", "container", "total_lines", "matching_lines", "returned_lines", "logs")
})

var ListWorkflowsResult = Type("ListWorkflowsResult", func() {
	Attribute("workflows", ArrayOf(WorkflowSummary))
	Attribute("count", Int)
	Attribute("namespace", String)
	Attribute("status", String)
	Attribute("source", String)
	Required("workflows", "count", "source")
})

var ActionResult = Type("ActionResult", func() {
	Attribute("status", String, "ok, dry_run, confirmation_required, or denied")
	Attribute("message", String)
	Attribute("namespace", String)
	Attribute("name", String)
	Attribute("reason", String)
	Attribute("preview", String)
	Attribute("instructions", String)
	Attribute("confirmation_token", String)
	Attribute("restart_successful", Boolean)
	Required("status", "message")
})

var CronWorkflowSummary = Type("CronWorkflowSummary", func() {
	Attribute("name", String)
	Attribute("namespace", String)
	Attribute("schedule", String)
	Attribute("suspended", Boolean)
	Required("name", "namespace")
})

var ListCronWorkflowsResult = Type("ListCronWorkflowsResult", func() {
	Attribute("cron_workflows", ArrayOf(CronWorkflowSummary))
	Attribute("count", Int)
	Attribute("namespace", String)
	Attribute("suspended", Boolean)
	Attribute("source", String)
	Required("cron_workflows", "count", "source")
})

var CronWorkflowDetailResult = Type("CronWorkflowDetailResult", func() {
	Attribute("name", String)
	Attribute("namespace", String)
	Attribute("schedule", String)
	Attribute("suspended", Boolean)
	Attribute("last_scheduled_time", String)
	Attribute("next_scheduled_time", String)
	Attribute("source", String)
	Required("name", "source")
})

var CronHistoryEntry = Type("CronHistoryEntry", func() {
	Attribute("name", String)
	Attribute("status", String)
	Attribute("started_at", String)
	Attribute("finished_at", String)
	Attribute("duration", String)
	Required("name")
})

var CronHistoryResult = Type("CronHistoryResult", func() {
	Attribute("name", String)
	Attribute("namespace", String)
	Attribute("history", ArrayOf(CronHistoryEntry))
	Attribute("count", Int)
	Attribute("source", String)
	Required("name", "history", "count", "source")
})

var TemplateSummary = Type("TemplateSummary", func() {
	Attribute("name", String)
	Attribute("namespace", String)
	Attribute("entrypoint", String)
	Required("name")
})

var ListWorkflowTemplatesResult = Type("ListWorkflowTemplatesResult", func() {
	Attribute("templates", ArrayOf(TemplateSummary))
	Attribute("count", Int)
	Attribute("namespace", String)
	Attribute("label_selector", String)
	Attribute("source", String)
	Required("templates", "count", "source")
})

var WorkflowTemplateDetailResult = Type("WorkflowTemplateDetailResult", func() {
	Attribute("name", String)
	Attribute("namespace", String)
	Attribute("entrypoint", String)
	Attribute("template_names", ArrayOf(String))
	Attribute("source", String)
	Required("name", "source")
})

var ClusterWorkflowTemplateSummary = Type("ClusterWorkflowTemplateSummary", func() {
	Attribute("name", String)
	Attribute("entrypoint", String)
	Required("name")
})

var ListClusterWorkflowTemplatesResult = Type("ListClusterWorkflowTemplatesResult", func() {
	Attribute("templates", ArrayOf(ClusterWorkflowTemplateSummary))
	Attribute("count", Int)
	Attribute("label_selector", String)
	Attribute("source", String)
	Required("templates", "count", "source")
})

var ClusterWorkflowTemplateDetailResult = Type("ClusterWorkflowTemplateDetailResult", func() {
	Attribute("name", String)
	Attribute("entrypoint", String)
	Attribute("template_names", ArrayOf(String))
	Attribute("source", String)
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

	MCP("go-argo-mcp", "0.1.0", ProtocolVersion("2025-06-18"))
	JSONRPC(func() { POST("/rpc") })

	Method("ListWorkflows", func() {
		Description("List workflows in a namespace with an optional status filter.")
		Meta("mcp:annotation:readOnlyHint", "true")
		Payload(func() {
			Attribute("namespace", String, "Kubernetes namespace to query")
			Attribute("status", String, "Optional workflow status filter", func() {
				Enum("Running", "Succeeded", "Failed", "Pending", "Error")
			})
			Attribute("limit", Int, "Maximum number of workflows to return", func() {
				Default(50)
				Minimum(1)
			})
		})
		Result(ListWorkflowsResult)
		Tool("list_workflows", "List workflows in specified namespace(s) with optional status filtering")
	})

	Method("GetWorkflow", func() {
		Description("Get detailed information about a specific workflow.")
		Meta("mcp:annotation:readOnlyHint", "true")
		Payload(func() {
			Attribute("namespace", String)
			Attribute("name", String)
			Required("name")
		})
		Result(WorkflowDetailResult)
		Tool("get_workflow", "Get detailed information about a specific workflow")
	})

	Method("GetWorkflowLogs", func() {
		Description("Get logs from a workflow's pods.")
		Meta("mcp:annotation:readOnlyHint", "true")
		Payload(func() {
			Attribute("namespace", String)
			Attribute("workflow_name", String)
			Attribute("pod_name", String)
			Attribute("container", String, func() { Default("main") })
			Attribute("search", String)
			Attribute("max_lines", Int, "Maximum lines to return; zero returns all lines", func() {
				Default(200)
				Minimum(0)
			})
			Required("workflow_name")
		})
		Result(WorkflowLogsResult)
		Tool("get_workflow_logs", "Get logs from a workflow's pods")
	})

	Method("TerminateWorkflow", func() {
		Description("Terminate a running workflow.")
		Meta("mcp:annotation:destructiveHint", "true")
		Payload(func() {
			Attribute("namespace", String)
			Attribute("name", String)
			Attribute("reason", String)
			Attribute("dry_run", Boolean, "Preview mode; defaults to true")
			Attribute("confirmation_token", String)
			Required("name", "reason")
		})
		Result(ActionResult)
		Tool("terminate_workflow", "Terminate a running workflow (DESTRUCTIVE - requires confirmation)")
	})

	Method("RetryWorkflow", func() {
		Description("Retry a failed workflow.")
		Meta("mcp:annotation:destructiveHint", "true")
		Payload(func() {
			Attribute("namespace", String)
			Attribute("name", String)
			Attribute("restart_successful", Boolean, "Also restart successful steps; defaults to false")
			Required("name")
		})
		Result(ActionResult)
		Tool("retry_workflow", "Retry a failed workflow")
	})

	Method("ListCronWorkflows", func() {
		Description("List CronWorkflows in namespace(s).")
		Meta("mcp:annotation:readOnlyHint", "true")
		Payload(func() {
			Attribute("namespace", String)
			Attribute("suspended", Boolean)
		})
		Result(ListCronWorkflowsResult)
		Tool("list_cron_workflows", "List CronWorkflows in namespace(s)")
	})

	Method("GetCronWorkflow", func() {
		Description("Get CronWorkflow details including schedule and last execution.")
		Meta("mcp:annotation:readOnlyHint", "true")
		Payload(func() {
			Attribute("namespace", String)
			Attribute("name", String)
			Required("name")
		})
		Result(CronWorkflowDetailResult)
		Tool("get_cron_workflow", "Get CronWorkflow details including schedule and last execution")
	})

	Method("GetCronHistory", func() {
		Description("Get execution history of a CronWorkflow.")
		Meta("mcp:annotation:readOnlyHint", "true")
		Payload(func() {
			Attribute("namespace", String)
			Attribute("name", String)
			Attribute("limit", Int, func() {
				Default(10)
				Minimum(1)
			})
			Required("name")
		})
		Result(CronHistoryResult)
		Tool("get_cron_history", "Get execution history of a CronWorkflow")
	})

	Method("ToggleCronSuspension", func() {
		Description("Suspend or resume a CronWorkflow.")
		Meta("mcp:annotation:destructiveHint", "true")
		Payload(func() {
			Attribute("namespace", String)
			Attribute("name", String)
			Attribute("suspend", Boolean)
			Required("name", "suspend")
		})
		Result(ActionResult)
		Tool("toggle_cron_suspension", "Suspend or resume a CronWorkflow")
	})

	Method("ListWorkflowTemplates", func() {
		Description("List WorkflowTemplates in namespace.")
		Meta("mcp:annotation:readOnlyHint", "true")
		Payload(func() {
			Attribute("namespace", String)
			Attribute("label_selector", String)
		})
		Result(ListWorkflowTemplatesResult)
		Tool("list_workflow_templates", "List WorkflowTemplates in namespace")
	})

	Method("GetWorkflowTemplate", func() {
		Description("Get WorkflowTemplate details.")
		Meta("mcp:annotation:readOnlyHint", "true")
		Payload(func() {
			Attribute("namespace", String)
			Attribute("name", String)
			Required("name")
		})
		Result(WorkflowTemplateDetailResult)
		Tool("get_workflow_template", "Get WorkflowTemplate details")
	})

	Method("ListClusterWorkflowTemplates", func() {
		Description("List ClusterWorkflowTemplates (cluster-scoped).")
		Meta("mcp:annotation:readOnlyHint", "true")
		Payload(func() {
			Attribute("label_selector", String)
		})
		Result(ListClusterWorkflowTemplatesResult)
		Tool("list_cluster_workflow_templates", "List ClusterWorkflowTemplates (cluster-scoped)")
	})

	Method("GetClusterWorkflowTemplate", func() {
		Description("Get ClusterWorkflowTemplate details.")
		Meta("mcp:annotation:readOnlyHint", "true")
		Payload(func() {
			Attribute("name", String)
			Required("name")
		})
		Result(ClusterWorkflowTemplateDetailResult)
		Tool("get_cluster_workflow_template", "Get ClusterWorkflowTemplate details")
	})
})
