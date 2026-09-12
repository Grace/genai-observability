resource "aws_s3_bucket" "corpus" { bucket_prefix = "${var.project}-corpus-"
  force_destroy = true }
resource "aws_s3_bucket_public_access_block" "corpus" {
  bucket = aws_s3_bucket.corpus.id
  block_public_acls = true
  block_public_policy = true
  ignore_public_acls = true
  restrict_public_buckets = true
}
resource "aws_s3_bucket_server_side_encryption_configuration" "corpus" {
  bucket = aws_s3_bucket.corpus.id
  rule { apply_server_side_encryption_by_default { sse_algorithm = "AES256" } }
}
resource "aws_s3_bucket_versioning" "corpus" { bucket = aws_s3_bucket.corpus.id
  versioning_configuration { status = "Enabled" } }

resource "aws_dynamodb_table" "evals" {
  name = "${var.project}-evals"
  billing_mode = "PAY_PER_REQUEST"
  hash_key = "pk"
  range_key = "sk"
  attribute { name = "pk"
  type = "S" }
  attribute { name = "sk"
  type = "S" }
  point_in_time_recovery { enabled = true }
}
resource "aws_cloudwatch_event_bus" "eval" { name = "${var.project}-eval" }
resource "aws_ecr_repository" "app" { name = "${var.project}-app"
  image_scanning_configuration { scan_on_push = true }
  force_delete = true }
resource "aws_ecr_lifecycle_policy" "app" {
  repository = aws_ecr_repository.app.name
  policy = jsonencode({ rules = [{ rulePriority = 1, description = "keep 10", selection = { tagStatus = "any", countType = "imageCountMoreThan", countNumber = 10 }, action = { type = "expire" } }] })
}
