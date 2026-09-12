resource "aws_iam_role" "lambda" {
  name_prefix = "${var.project}-lambda-"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "lambda.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy" "lambda" {
  role = aws_iam_role.lambda.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      { Effect = "Allow", Action = ["logs:CreateLogGroup", "logs:CreateLogStream", "logs:PutLogEvents"], Resource = "*" },
      { Effect = "Allow", Action = ["bedrock:InvokeModel"], Resource = "*" },
      { Effect = "Allow", Action = ["s3:PutObject", "s3:GetObject"], Resource = "${aws_s3_bucket.corpus.arn}/*" },
      { Effect = "Allow", Action = ["s3:ListBucket"], Resource = aws_s3_bucket.corpus.arn },
      { Effect = "Allow", Action = ["dynamodb:PutItem", "dynamodb:GetItem", "dynamodb:UpdateItem"], Resource = aws_dynamodb_table.evals.arn },
      { Effect = "Allow", Action = ["events:PutEvents"], Resource = aws_cloudwatch_event_bus.eval.arn },
      { Effect = "Allow", Action = ["secretsmanager:GetSecretValue"], Resource = var.honeycomb_api_key_secret_arn }
    ]
  })
}

locals {
  lambda_specs = {
    schema-eval       = { file = "schema-eval", timeout = 15, memory = 256 }
    policy-eval       = { file = "policy-eval", timeout = 15, memory = 256 }
    oracle-eval-a     = { file = "oracle-eval-a", timeout = 60, memory = 512 }
    oracle-eval-b     = { file = "oracle-eval-b", timeout = 60, memory = 512 }
    consensus         = { file = "consensus", timeout = 15, memory = 256 }
    normalizer        = { file = "normalizer", timeout = 30, memory = 512 }
    replay-worker     = { file = "replay-worker", timeout = 90, memory = 512 }
    mapping-semantic  = { file = "mapping-semantic", timeout = 60, memory = 512 }
    mapping-otel      = { file = "mapping-otel", timeout = 60, memory = 512 }
    mapping-loss      = { file = "mapping-loss", timeout = 60, memory = 512 }
    mapping-reviewer  = { file = "mapping-reviewer", timeout = 60, memory = 512 }
    mapping-aggregate = { file = "mapping-aggregate", timeout = 15, memory = 256 }
  }
}

resource "aws_lambda_function" "fn" {
  for_each         = local.lambda_specs
  function_name    = "${var.project}-${each.key}"
  role             = aws_iam_role.lambda.arn
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  handler          = "bootstrap"
  filename         = "${path.module}/../build/${each.value.file}.zip"
  source_code_hash = filebase64sha256("${path.module}/../build/${each.value.file}.zip")
  timeout          = each.value.timeout
  memory_size      = each.value.memory

  environment {
    variables = {
      CORPUS_BUCKET                = aws_s3_bucket.corpus.bucket
      EVAL_TABLE                   = aws_dynamodb_table.evals.name
      EVENT_BUS_NAME               = aws_cloudwatch_event_bus.eval.name
      HONEYCOMB_OTLP_ENDPOINT      = replace(replace(var.honeycomb_otlp_endpoint, "https://", ""), "http://", "")
      HONEYCOMB_API_KEY_SECRET_ARN = var.honeycomb_api_key_secret_arn
      REPLAY_MODEL_ID              = var.replay_model_id
      MODEL_PRICING_JSON           = var.model_pricing_json
      BEDROCK_MODEL_ID             = each.key == "oracle-eval-a" ? var.oracle_model_id_a : each.key == "oracle-eval-b" ? var.oracle_model_id_b : startswith(each.key, "mapping-") && each.key != "mapping-aggregate" ? var.mapping_model_id : ""
      JUDGE_NAME                   = each.key == "oracle-eval-a" ? "oracle-a" : each.key == "oracle-eval-b" ? "oracle-b" : ""
      MAPPING_AGENT_ROLE           = each.key == "mapping-semantic" ? "semantic" : each.key == "mapping-otel" ? "otel" : each.key == "mapping-loss" ? "loss" : each.key == "mapping-reviewer" ? "reviewer" : ""
    }
  }
}

# Implicit Lambda log groups never expire, which is both a cost leak and a
# reason nobody notices they are empty.
resource "aws_cloudwatch_log_group" "lambda" {
  for_each          = local.lambda_specs
  name              = "/aws/lambda/${var.project}-${each.key}"
  retention_in_days = 14
}
