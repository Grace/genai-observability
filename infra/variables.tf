variable "aws_region" {
  type    = string
  default = "us-east-1"
}

variable "project" {
  type    = string
  default = "genai-observability"
}

variable "candidate_model_id" {
  type        = string
  description = "Bedrock Converse-compatible model/inference profile for the production app"
}

variable "oracle_model_id_a" {
  type        = string
  description = "First Bedrock oracle model/inference profile"
}

variable "oracle_model_id_b" {
  type        = string
  description = "Second Bedrock oracle model/inference profile (may equal A)"
}

variable "replay_model_id" {
  type        = string
  description = "Default Bedrock replay model"
}

variable "mapping_model_id" {
  type        = string
  description = "Bedrock model used by semantic mapping agents"
}

variable "honeycomb_api_key_secret_arn" {
  type        = string
  description = "Secrets Manager ARN whose secret value is the Honeycomb API key"
}

variable "honeycomb_otlp_endpoint" {
  type    = string
  default = "https://api.honeycomb.io"
}

variable "app_image_tag" {
  type    = string
  default = "latest"
}

variable "desired_count" {
  type    = number
  default = 1
}

variable "enable_deletion_protection" {
  type    = bool
  default = false
}

variable "model_pricing_json" {
  type        = string
  description = "Versioned JSON pricing catalog used to calculate input/output token cost. Replace sample rates before production use."
  default     = ""
}

variable "allowed_cidrs" {
  type        = list(string)
  description = "CIDR blocks permitted to reach the ALB. The endpoint is unauthenticated and spends Bedrock tokens per request, so this must stay narrow. Set to [\"<your-ip>/32\"]; 0.0.0.0/0 is rejected."
  default     = []

  validation {
    condition     = length(var.allowed_cidrs) > 0 && !contains(var.allowed_cidrs, "0.0.0.0/0")
    error_message = "Set allowed_cidrs to at least one specific CIDR and not 0.0.0.0/0. Example: allowed_cidrs = [\"203.0.113.4/32\"]."
  }
}

variable "web_domain" {
  type        = string
  description = "Public hostname for the Normalization Inspector, e.g. genai-observability.wirewitch.ai. Empty disables all public hosting."
  default     = ""
}

variable "api_domain" {
  type        = string
  description = "Hostname for the authenticated API. /ask is served here behind a Cognito login. Required when web_domain is set."
  default     = ""
}

variable "cognito_domain_prefix" {
  type        = string
  description = "Globally unique prefix for the Cognito hosted login domain."
  default     = ""
}

variable "waf_rate_limit" {
  type        = number
  description = "Requests per five minutes from a single IP before WAF blocks it. AWS enforces a floor of 100."
  default     = 500

  validation {
    condition     = var.waf_rate_limit >= 100
    error_message = "WAF rate-based rules require a limit of at least 100."
  }
}

variable "budget_alert_email" {
  type        = string
  description = "Address to notify on budget thresholds. Empty disables the budget."
  default     = ""
}

variable "budget_limit_usd" {
  type        = string
  description = "Monthly budget in USD. Alerts at 80 percent forecast and 100 percent actual."
  default     = "100"
}
