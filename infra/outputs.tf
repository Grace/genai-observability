output "alb_url" {
  value = "http://${aws_lb.app.dns_name}"
}

output "ecr_repository_url" {
  value = aws_ecr_repository.app.repository_url
}

output "event_bus_name" {
  value = aws_cloudwatch_event_bus.eval.name
}

output "evaluation_state_machine_arn" {
  value = aws_sfn_state_machine.eval.arn
}

output "replay_state_machine_arn" {
  value = aws_sfn_state_machine.replay.arn
}

output "mapping_state_machine_arn" {
  value = aws_sfn_state_machine.mapping.arn
}

output "corpus_bucket" {
  value = aws_s3_bucket.corpus.bucket
}

output "eval_table" {
  value = aws_dynamodb_table.evals.name
}
