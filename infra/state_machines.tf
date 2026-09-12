resource "aws_iam_role" "sfn" {
  name_prefix = "${var.project}-sfn-"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "states.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy" "sfn" {
  role = aws_iam_role.sfn.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      { Effect = "Allow", Action = ["lambda:InvokeFunction"], Resource = [for f in aws_lambda_function.fn : f.arn] },
      { Effect = "Allow", Action = ["s3:ListBucket"], Resource = aws_s3_bucket.corpus.arn },
      { Effect = "Allow", Action = ["s3:GetObject", "s3:PutObject"], Resource = "${aws_s3_bucket.corpus.arn}/*" },
      { Effect = "Allow", Action = ["states:StartExecution", "states:DescribeExecution"], Resource = "*" }
    ]
  })
}

resource "aws_sfn_state_machine" "eval" {
  name     = "${var.project}-evaluation"
  role_arn = aws_iam_role.sfn.arn
  definition = jsonencode({
    Comment = "GenAI Observability evaluation harness"
    StartAt = "Evaluate"
    States = {
      Evaluate = {
        Type = "Parallel"
        Branches = [
          { StartAt = "Schema", States = { Schema = { Type = "Task", Resource = "arn:aws:states:::lambda:invoke", Parameters = { FunctionName = aws_lambda_function.fn["schema-eval"].arn, "Payload.$" = "$" }, OutputPath = "$.Payload", End = true } } },
          { StartAt = "Policy", States = { Policy = { Type = "Task", Resource = "arn:aws:states:::lambda:invoke", Parameters = { FunctionName = aws_lambda_function.fn["policy-eval"].arn, "Payload.$" = "$" }, OutputPath = "$.Payload", End = true } } },
          { StartAt = "OracleA", States = { OracleA = { Type = "Task", Resource = "arn:aws:states:::lambda:invoke", Parameters = { FunctionName = aws_lambda_function.fn["oracle-eval-a"].arn, "Payload.$" = "$" }, OutputPath = "$.Payload", Retry = [{ ErrorEquals = ["Lambda.ServiceException", "Lambda.AWSLambdaException", "Lambda.SdkClientException"], IntervalSeconds = 2, MaxAttempts = 3, BackoffRate = 2 }], End = true } } },
          { StartAt = "OracleB", States = { OracleB = { Type = "Task", Resource = "arn:aws:states:::lambda:invoke", Parameters = { FunctionName = aws_lambda_function.fn["oracle-eval-b"].arn, "Payload.$" = "$" }, OutputPath = "$.Payload", Retry = [{ ErrorEquals = ["Lambda.ServiceException", "Lambda.AWSLambdaException", "Lambda.SdkClientException"], IntervalSeconds = 2, MaxAttempts = 3, BackoffRate = 2 }], End = true } } }
        ]
        ResultPath = "$.results"
        Next       = "Consensus"
      }
      Consensus = {
        Type       = "Task"
        Resource   = "arn:aws:states:::lambda:invoke"
        Parameters = { FunctionName = aws_lambda_function.fn["consensus"].arn, "Payload.$" = "$.results" }
        ResultPath = "$.consensus_raw"
        Next       = "Normalize"
      }
      Normalize = {
        Type     = "Task"
        Resource = "arn:aws:states:::lambda:invoke"
        Parameters = {
          FunctionName = aws_lambda_function.fn["normalizer"].arn
          Payload = {
            "request.$"   = "$"
            "results.$"   = "$.results"
            "consensus.$" = "$.consensus_raw.Payload"
          }
        }
        OutputPath = "$.Payload"
        End        = true
      }
    }
  })
}

resource "aws_sfn_state_machine" "replay" {
  name     = "${var.project}-replay"
  role_arn = aws_iam_role.sfn.arn
  definition = jsonencode({
    Comment = "Distributed counterfactual replay over the production corpus"
    StartAt = "ReplayCorpus"
    States = {
      ReplayCorpus = {
        Type           = "Map"
        Label          = "ReplayCorpus"
        MaxConcurrency = 25
        ItemReader = {
          Resource   = "arn:aws:states:::s3:listObjectsV2"
          Parameters = { Bucket = aws_s3_bucket.corpus.bucket, Prefix = "corpus/production/" }
        }
        ItemSelector = {
          bucket             = aws_s3_bucket.corpus.bucket
          "key.$"            = "$$.Map.Item.Value.Key"
          "model.$"          = "$$.Execution.Input.model"
          "prompt_version.$" = "$$.Execution.Input.prompt_version"
        }
        ItemProcessor = {
          ProcessorConfig = { Mode = "DISTRIBUTED", ExecutionType = "EXPRESS" }
          StartAt         = "ReplayOne"
          States = {
            ReplayOne = {
              Type       = "Task"
              Resource   = "arn:aws:states:::lambda:invoke"
              Parameters = { FunctionName = aws_lambda_function.fn["replay-worker"].arn, "Payload.$" = "$" }
              OutputPath = "$.Payload"
              End        = true
            }
          }
        }
        ResultWriter = {
          Resource   = "arn:aws:states:::s3:putObject"
          Parameters = { Bucket = aws_s3_bucket.corpus.bucket, Prefix = "replay-runs" }
        }
        End = true
      }
    }
  })
}

resource "aws_cloudwatch_event_rule" "eval" {
  name           = "${var.project}-eval-request"
  event_bus_name = aws_cloudwatch_event_bus.eval.name
  event_pattern = jsonencode({
    source        = ["genai.observability.app", "genai.observability.replay"]
    "detail-type" = ["EvaluationRequested"]
  })
}

resource "aws_iam_role" "eventbridge" {
  name_prefix = "${var.project}-events-"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "events.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy" "eventbridge" {
  role = aws_iam_role.eventbridge.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["states:StartExecution"]
      Resource = aws_sfn_state_machine.eval.arn
    }]
  })
}

resource "aws_cloudwatch_event_target" "sfn" {
  rule           = aws_cloudwatch_event_rule.eval.name
  event_bus_name = aws_cloudwatch_event_bus.eval.name
  arn            = aws_sfn_state_machine.eval.arn
  role_arn       = aws_iam_role.eventbridge.arn

  input_transformer {
    input_paths    = { detail = "$.detail" }
    input_template = "<detail>"
  }
}

resource "aws_sfn_state_machine" "mapping" {
  name     = "${var.project}-semantic-mapping"
  role_arn = aws_iam_role.sfn.arn
  definition = jsonencode({
    Comment = "Agent-assisted semantic mapping analysis; advisory only"
    StartAt = "Analyze"
    States = {
      Analyze = {
        Type = "Parallel"
        Branches = [
          { StartAt = "Semantic", States = { Semantic = { Type = "Task", Resource = "arn:aws:states:::lambda:invoke", Parameters = { FunctionName = aws_lambda_function.fn["mapping-semantic"].arn, "Payload.$" = "$" }, OutputPath = "$.Payload", End = true } } },
          { StartAt = "OTel", States = { OTel = { Type = "Task", Resource = "arn:aws:states:::lambda:invoke", Parameters = { FunctionName = aws_lambda_function.fn["mapping-otel"].arn, "Payload.$" = "$" }, OutputPath = "$.Payload", End = true } } },
          { StartAt = "Loss", States = { Loss = { Type = "Task", Resource = "arn:aws:states:::lambda:invoke", Parameters = { FunctionName = aws_lambda_function.fn["mapping-loss"].arn, "Payload.$" = "$" }, OutputPath = "$.Payload", End = true } } }
        ]
        ResultPath = "$.assessments"
        Next       = "AdversarialReview"
      }
      AdversarialReview = {
        Type     = "Task"
        Resource = "arn:aws:states:::lambda:invoke"
        Parameters = {
          FunctionName = aws_lambda_function.fn["mapping-reviewer"].arn
          Payload = {
            "request.$"     = "$"
            "assessments.$" = "$.assessments"
          }
        }
        ResultPath = "$.reviewer_raw"
        Next       = "Aggregate"
      }
      Aggregate = {
        Type     = "Task"
        Resource = "arn:aws:states:::lambda:invoke"
        Parameters = {
          FunctionName = aws_lambda_function.fn["mapping-aggregate"].arn
          Payload = {
            "request.$"     = "$"
            "assessments.$" = "$.assessments"
            "reviewer.$"    = "$.reviewer_raw.Payload"
          }
        }
        OutputPath = "$.Payload"
        End        = true
      }
    }
  })
}
