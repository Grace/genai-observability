// Public hosting for the Normalization Inspector.
//
// One hostname fronts both the static app and the normalization API, so the
// browser makes same-origin requests: no CORS, and no mixed content from an
// HTTPS page calling an HTTP load balancer.
//
// Only /normalize is published. /ask invokes Bedrock on every request and has no
// authentication, so exposing it on a public hostname would let anyone spend the
// account's token budget. It stays reachable only from allowed_cidrs, directly
// against the load balancer.
//
// Everything here is created only when web_domain is set.

locals {
  web_enabled = var.web_domain != "" ? 1 : 0
  s3_origin   = "s3-inspector"
  alb_origin  = "alb-api"
}

# One certificate covers both hostnames. CloudFront only accepts certificates
# from us-east-1, and the load balancer is in us-east-1, so they can share it.
resource "aws_acm_certificate" "web" {
  count                     = local.web_enabled
  domain_name               = var.web_domain
  subject_alternative_names = var.api_domain != "" ? [var.api_domain] : []
  validation_method         = "DNS"

  lifecycle {
    create_before_destroy = true
  }
}

# The validation record is added by hand in Cloudflare, which manages DNS for
# this domain. Terraform waits for it rather than creating it.
resource "aws_acm_certificate_validation" "web" {
  count           = local.web_enabled
  certificate_arn = aws_acm_certificate.web[0].arn
  validation_record_fqdns = [
    for o in aws_acm_certificate.web[0].domain_validation_options : o.resource_record_name
  ]

  timeouts {
    create = "60m"
  }
}

resource "aws_s3_bucket" "web" {
  count         = local.web_enabled
  bucket_prefix = "${var.project}-web-"
  force_destroy = true
}

resource "aws_s3_bucket_public_access_block" "web" {
  count                   = local.web_enabled
  bucket                  = aws_s3_bucket.web[0].id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_server_side_encryption_configuration" "web" {
  count  = local.web_enabled
  bucket = aws_s3_bucket.web[0].id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

# Origin access control keeps the bucket private: only CloudFront can read it.
resource "aws_cloudfront_origin_access_control" "web" {
  count                             = local.web_enabled
  name                              = "${var.project}-web-oac"
  origin_access_control_origin_type = "s3"
  signing_behavior                  = "always"
  signing_protocol                  = "sigv4"
}

resource "aws_s3_bucket_policy" "web" {
  count  = local.web_enabled
  bucket = aws_s3_bucket.web[0].id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "cloudfront.amazonaws.com" }
      Action    = "s3:GetObject"
      Resource  = "${aws_s3_bucket.web[0].arn}/*"
      Condition = {
        StringEquals = { "AWS:SourceArn" = aws_cloudfront_distribution.web[0].arn }
      }
    }]
  })
}

resource "aws_cloudfront_distribution" "web" {
  count               = local.web_enabled
  enabled             = true
  default_root_object = "index.html"
  aliases             = [var.web_domain]
  comment             = "${var.project} normalization inspector"
  price_class         = "PriceClass_100"

  origin {
    origin_id                = local.s3_origin
    domain_name              = aws_s3_bucket.web[0].bucket_regional_domain_name
    origin_access_control_id = aws_cloudfront_origin_access_control.web[0].id
  }

  origin {
    origin_id   = local.alb_origin
    domain_name = aws_lb.app.dns_name

    custom_origin_config {
      http_port  = 80
      https_port = 443
      # The load balancer has no TLS listener. This hop is inside AWS, and the
      # viewer-facing connection is HTTPS regardless.
      origin_protocol_policy = "http-only"
      origin_ssl_protocols   = ["TLSv1.2"]
    }
  }

  default_cache_behavior {
    target_origin_id       = local.s3_origin
    viewer_protocol_policy = "redirect-to-https"
    allowed_methods        = ["GET", "HEAD", "OPTIONS"]
    cached_methods         = ["GET", "HEAD"]
    compress               = true
    # Managed-CachingOptimized
    cache_policy_id = "658327ea-f89d-4fab-a63d-7e88639e58f6"
  }

  # Only the normalization endpoint is published. It is pure computation: it
  # calls no model and spends nothing per request.
  ordered_cache_behavior {
    path_pattern           = "/normalize"
    target_origin_id       = local.alb_origin
    viewer_protocol_policy = "https-only"
    allowed_methods        = ["GET", "HEAD", "OPTIONS", "PUT", "POST", "PATCH", "DELETE"]
    cached_methods         = ["GET", "HEAD"]
    compress               = true
    # Managed-CachingDisabled: responses depend entirely on the request body.
    cache_policy_id = "4135ea2d-6df8-44a3-9df3-4b5a84be39ad"
    # Managed-AllViewerExceptHostHeader: forwards the body and headers the
    # origin needs without breaking host-based routing.
    origin_request_policy_id = "b689b0a8-53d0-40ab-baf2-68738e2966ac"
  }

  # /ask is not served here, but it must say so. Without this behaviour it fell
  # through to the S3 default and the SPA error-page rule turned S3's 403 into
  # "200 text/html", so a POST to an API path answered with a web page. Routing
  # it to the load balancer reuses the listener rule that already returns 403
  # naming the authenticated hostname.
  ordered_cache_behavior {
    path_pattern             = "/ask"
    target_origin_id         = local.alb_origin
    viewer_protocol_policy   = "https-only"
    allowed_methods          = ["GET", "HEAD", "OPTIONS", "PUT", "POST", "PATCH", "DELETE"]
    cached_methods           = ["GET", "HEAD"]
    cache_policy_id          = "4135ea2d-6df8-44a3-9df3-4b5a84be39ad"
    origin_request_policy_id = "b689b0a8-53d0-40ab-baf2-68738e2966ac"
  }

  # No custom_error_response blocks. CloudFront applies them to every origin, so
  # an SPA fallback that rewrites 403 to "200 /index.html" also rewrites the load
  # balancer's 403 for /ask, and would mask genuine errors from /normalize. This
  # app is a single page with no client-side routes, so the fallback bought
  # nothing and cost the ability to return an honest status code.

  web_acl_id = aws_wafv2_web_acl.web[0].arn

  restrictions {
    geo_restriction {
      restriction_type = "none"
    }
  }

  viewer_certificate {
    acm_certificate_arn      = aws_acm_certificate_validation.web[0].certificate_arn
    ssl_support_method       = "sni-only"
    minimum_protocol_version = "TLSv1.2_2021"
  }
}

output "web_url" {
  value       = local.web_enabled == 1 ? "https://${var.web_domain}" : "(web_domain not set)"
  description = "Public URL of the Normalization Inspector."
}

output "web_bucket" {
  value       = local.web_enabled == 1 ? aws_s3_bucket.web[0].id : ""
  description = "S3 bucket the built inspector is uploaded to."
}

output "cloudfront_domain" {
  value       = local.web_enabled == 1 ? aws_cloudfront_distribution.web[0].domain_name : ""
  description = "CNAME target for the public hostname."
}

output "acm_validation_records" {
  value = local.web_enabled == 1 ? [
    for o in aws_acm_certificate.web[0].domain_validation_options : {
      name  = o.resource_record_name
      type  = o.resource_record_type
      value = o.resource_record_value
    }
  ] : []
  description = "DNS records to create in Cloudflare so the certificate can be issued."
}
