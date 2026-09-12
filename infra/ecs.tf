resource "aws_cloudwatch_log_group" "app" { name = "/ecs/${var.project}/app"
  retention_in_days = 14 }
resource "aws_cloudwatch_log_group" "otel" { name = "/ecs/${var.project}/otel"
  retention_in_days = 14 }
resource "aws_ecs_cluster" "main" { name = "${var.project}-cluster" }

resource "aws_iam_role" "ecs_execution" {
  name_prefix = "${var.project}-ecs-exec-"
  assume_role_policy = jsonencode({ Version = "2012-10-17", Statement = [{ Effect = "Allow", Principal = { Service = "ecs-tasks.amazonaws.com" }, Action = "sts:AssumeRole" }] })
}
resource "aws_iam_role_policy_attachment" "ecs_execution" { role = aws_iam_role.ecs_execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy" }
resource "aws_iam_role_policy" "ecs_execution_secret" {
  role = aws_iam_role.ecs_execution.id
  policy = jsonencode({ Version = "2012-10-17", Statement = [{ Effect = "Allow", Action = ["secretsmanager:GetSecretValue"], Resource = var.honeycomb_api_key_secret_arn }] })
}
resource "aws_iam_role" "ecs_task" {
  name_prefix = "${var.project}-ecs-task-"
  assume_role_policy = jsonencode({ Version = "2012-10-17", Statement = [{ Effect = "Allow", Principal = { Service = "ecs-tasks.amazonaws.com" }, Action = "sts:AssumeRole" }] })
}
resource "aws_iam_role_policy" "ecs_task" {
  role = aws_iam_role.ecs_task.id
  policy = jsonencode({ Version = "2012-10-17", Statement = [
    { Effect = "Allow", Action = ["bedrock:InvokeModel"], Resource = "*" },
    { Effect = "Allow", Action = ["events:PutEvents"], Resource = aws_cloudwatch_event_bus.eval.arn }
  ] })
}

locals {
  otel_config = <<-YAML
    receivers:
      otlp:
        protocols:
          grpc:
            endpoint: 0.0.0.0:4317
          http:
            endpoint: 0.0.0.0:4318
    processors:
      batch: {}
    exporters:
      otlphttp/honeycomb:
        endpoint: ${var.honeycomb_otlp_endpoint}
        headers:
          x-honeycomb-team: $${env:HONEYCOMB_API_KEY}
    service:
      pipelines:
        traces:
          receivers: [otlp]
          processors: [batch]
          exporters: [otlphttp/honeycomb]
  YAML
}

resource "aws_ecs_task_definition" "app" {
  family = var.project
  network_mode = "awsvpc"
  requires_compatibilities = ["FARGATE"]
  cpu = "1024"
  memory = "2048"
  execution_role_arn = aws_iam_role.ecs_execution.arn
  task_role_arn = aws_iam_role.ecs_task.arn
  container_definitions = jsonencode([
    {
      name = "app", image = "${aws_ecr_repository.app.repository_url}:${var.app_image_tag}", essential = true,
      portMappings = [{ containerPort = 8080, hostPort = 8080, protocol = "tcp" }],
      environment = [
        { name = "EVENT_BUS_NAME", value = aws_cloudwatch_event_bus.eval.name },
        { name = "CANDIDATE_MODEL_ID", value = var.candidate_model_id },
        { name = "OTEL_EXPORTER_OTLP_ENDPOINT", value = "localhost:4317" }
      ],
      dependsOn = [{ containerName = "otel", condition = "START" }],
      logConfiguration = { logDriver = "awslogs", options = { "awslogs-group" = aws_cloudwatch_log_group.app.name, "awslogs-region" = var.aws_region, "awslogs-stream-prefix" = "app" } }
    },
    {
      name = "otel", image = "public.ecr.aws/aws-observability/aws-otel-collector:latest", essential = true,
      command = ["--config=env:AOT_CONFIG_CONTENT"],
      environment = [{ name = "AOT_CONFIG_CONTENT", value = local.otel_config }],
      secrets = [{ name = "HONEYCOMB_API_KEY", valueFrom = var.honeycomb_api_key_secret_arn }],
      logConfiguration = { logDriver = "awslogs", options = { "awslogs-group" = aws_cloudwatch_log_group.otel.name, "awslogs-region" = var.aws_region, "awslogs-stream-prefix" = "otel" } }
    }
  ])
}

resource "aws_ecs_service" "app" {
  name = "${var.project}-service"
  cluster = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.app.arn
  desired_count = var.desired_count
  launch_type = "FARGATE"
  network_configuration { subnets = aws_subnet.public[*].id
  security_groups = [aws_security_group.app.id]
  assign_public_ip = true }
  load_balancer { target_group_arn = aws_lb_target_group.app.arn
  container_name = "app"
  container_port = 8080 }
  depends_on = [aws_lb_listener.http]
}
