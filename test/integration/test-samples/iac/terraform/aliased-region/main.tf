terraform {
  required_providers {
    aws = {
      source = "hashicorp/aws"
    }
  }
}

provider "aws" {
  region = "eu-central-1"
}

provider "aws" {
  alias  = "us_east_1"
  region = "us-east-1"
}

resource "aws_sns_topic" "default" {
  name = "lstk-alias-repro-default"
}

resource "aws_sns_topic" "us_east_1" {
  provider = aws.us_east_1
  name     = "lstk-alias-repro-us-east-1"
}

output "topic_default" {
  value = aws_sns_topic.default.arn
}

output "topic_us_east_1" {
  value = aws_sns_topic.us_east_1.arn
}
