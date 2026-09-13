// Protection for the publicly reachable endpoints.
//
// Three independent layers, because any one of them alone leaves a gap:
//
//   1. The load balancer accepts traffic only from CloudFront (or the operator),
//      so nobody can reach the origin directly and skip everything below.
//   2. A rate-based WAF rule caps how hard a single address can hit the CDN.
//   3. /ask sits behind a Cognito login, because it invokes a model on every
//      request and an unauthenticated one is a way to spend someone else's money.
//
// A budget alarm bounds the damage if all three are wrong.

# ---------------------------------------------------------------------------
# 1. Origin lockdown
# ---------------------------------------------------------------------------

# CloudFront's published egress ranges. Membership is not identity - any
# CloudFront distribution, including someone else's, comes from these addresses -
# so this narrows exposure rather than authenticating it.
data "aws_ec2_managed_prefix_list" "cloudfront" {
  count = local.web_enabled
  name  = "com.amazonaws.global.cloudfront.origin-facing"
}

resource "aws_security_group_rule" "alb_from_cloudfront" {
  count             = local.web_enabled
  type              = "ingress"
  security_group_id = aws_security_group.alb.id
  from_port         = 80
  to_port           = 80
  protocol          = "tcp"
  prefix_list_ids   = [data.aws_ec2_managed_prefix_list.cloudfront[0].id]
  description       = "CloudFront origin-facing ranges, for /normalize"
}

resource "aws_security_group_rule" "alb_https_from_allowed" {
  count             = local.web_enabled
  type              = "ingress"
  security_group_id = aws_security_group.alb.id
  from_port         = 443
  to_port           = 443
  protocol          = "tcp"
  cidr_blocks       = ["0.0.0.0/0"]
  description       = "HTTPS for the Cognito-authenticated /ask; auth happens at the listener"
}

# ---------------------------------------------------------------------------
# 2. Rate limiting
# ---------------------------------------------------------------------------

resource "aws_wafv2_web_acl" "web" {
  count = local.web_enabled
  name  = "${var.project}-web-acl"
  # CloudFront web ACLs must be created in us-east-1 with CLOUDFRONT scope.
  scope = "CLOUDFRONT"

  default_action {
    allow {}
  }

  rule {
    name     = "rate-limit-per-ip"
    priority = 1

    action {
      block {}
    }

    statement {
      rate_based_statement {
        limit              = var.waf_rate_limit
        aggregate_key_type = "IP"
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      metric_name                = "${var.project}-rate-limit"
      sampled_requests_enabled   = true
    }
  }

  visibility_config {
    cloudwatch_metrics_enabled = true
    metric_name                = "${var.project}-web-acl"
    sampled_requests_enabled   = true
  }
}

# ---------------------------------------------------------------------------
# 3. Authentication on /ask
# ---------------------------------------------------------------------------

resource "aws_cognito_user_pool" "app" {
  count = local.web_enabled
  name  = "${var.project}-users"

  password_policy {
    minimum_length                   = 12
    require_lowercase                = true
    require_numbers                  = true
    require_symbols                  = true
    require_uppercase                = true
    temporary_password_validity_days = 7
  }

  # Self-signup would defeat the point: anyone could register and spend tokens.
  admin_create_user_config {
    allow_admin_create_user_only = true
  }
}

resource "aws_cognito_user_pool_domain" "app" {
  count        = local.web_enabled
  domain       = var.cognito_domain_prefix
  user_pool_id = aws_cognito_user_pool.app[0].id
}

resource "aws_cognito_user_pool_client" "alb" {
  count        = local.web_enabled
  name         = "${var.project}-alb"
  user_pool_id = aws_cognito_user_pool.app[0].id

  generate_secret                      = true
  allowed_oauth_flows                  = ["code"]
  allowed_oauth_flows_user_pool_client = true
  allowed_oauth_scopes                 = ["openid", "email"]
  supported_identity_providers         = ["COGNITO"]
  # The load balancer completes the OAuth exchange at this fixed path.
  callback_urls = ["https://${var.api_domain}/oauth2/idpresponse"]
}

# authenticate-cognito is only permitted on an HTTPS listener: the action sets a
# session cookie, which AWS will not do over plaintext.
resource "aws_lb_listener" "https" {
  count             = local.web_enabled
  load_balancer_arn = aws_lb.app.arn
  port              = 443
  protocol          = "HTTPS"
  ssl_policy        = "ELBSecurityPolicy-TLS13-1-2-2021-06"
  certificate_arn   = aws_acm_certificate_validation.web[0].certificate_arn

  # Anything without its own rule is refused rather than quietly served.
  default_action {
    type = "fixed-response"

    fixed_response {
      content_type = "text/plain"
      message_body = "Not found"
      status_code  = "404"
    }
  }
}

resource "aws_lb_listener_rule" "ask_authenticated" {
  count        = local.web_enabled
  listener_arn = aws_lb_listener.https[0].arn
  priority     = 10

  action {
    type = "authenticate-cognito"

    authenticate_cognito {
      user_pool_arn       = aws_cognito_user_pool.app[0].arn
      user_pool_client_id = aws_cognito_user_pool_client.alb[0].id
      user_pool_domain    = aws_cognito_user_pool_domain.app[0].domain
      session_timeout     = 3600
      scope               = "openid email"
    }
  }

  action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.app.arn
  }

  condition {
    path_pattern {
      values = ["/ask"]
    }
  }
}

# ---------------------------------------------------------------------------
# Blast radius
# ---------------------------------------------------------------------------

resource "aws_budgets_budget" "monthly" {
  count        = var.budget_alert_email != "" ? 1 : 0
  name         = "${var.project}-monthly"
  budget_type  = "COST"
  limit_amount = var.budget_limit_usd
  limit_unit   = "USD"
  time_unit    = "MONTHLY"

  # Forecast first, so the warning arrives while there is still time to act.
  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 80
    threshold_type             = "PERCENTAGE"
    notification_type          = "FORECASTED"
    subscriber_email_addresses = [var.budget_alert_email]
  }

  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 100
    threshold_type             = "PERCENTAGE"
    notification_type          = "ACTUAL"
    subscriber_email_addresses = [var.budget_alert_email]
  }
}

output "cognito_login_url" {
  value       = local.web_enabled == 1 ? "https://${aws_cognito_user_pool_domain.app[0].domain}.auth.${var.aws_region}.amazoncognito.com/login?client_id=${aws_cognito_user_pool_client.alb[0].id}&response_type=code&scope=openid+email&redirect_uri=https://${var.api_domain}/oauth2/idpresponse" : ""
  description = "Cognito hosted login. Create a user with: aws cognito-idp admin-create-user"
}

output "api_url" {
  value       = local.web_enabled == 1 ? "https://${var.api_domain}" : ""
  description = "Authenticated API hostname. /ask requires a Cognito login."
}

# The CloudFront prefix list is a range, not an identity: anyone's distribution
# can originate from it. Without this rule, a stranger could point their own
# distribution at this load balancer and reach /ask on port 80, skipping the
# Cognito listener entirely. Port 80 therefore serves only the endpoints that
# spend nothing.
resource "aws_lb_listener_rule" "block_ask_on_http" {
  count        = local.web_enabled
  listener_arn = aws_lb_listener.http.arn
  priority     = 5

  action {
    type = "fixed-response"

    fixed_response {
      content_type = "text/plain"
      message_body = "/ask requires authentication; use https://${var.api_domain}/ask"
      status_code  = "403"
    }
  }

  condition {
    path_pattern {
      values = ["/ask"]
    }
  }
}
