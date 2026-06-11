// Minimal CDK app used by the lstk cdk end-to-end tests: a single S3 bucket in
// one stack. Deploying it against LocalStack proves lstk routed CloudFormation
// and the S3 asset staging through the injected LocalStack endpoint.
const { App, Stack } = require("aws-cdk-lib");
const { Bucket } = require("aws-cdk-lib/aws-s3");

const app = new App();
const stack = new Stack(app, "LstkCdkE2eStack");
new Bucket(stack, "Bucket", { bucketName: "lstk-cdk-e2e-bucket" });
app.synth();
