// CDK app used by the lstk cdk end-to-end tests to exercise S3 asset publishing:
// a single Lambda function whose code is loaded with Code.fromAsset, which forces
// CDK to zip the lambda/ directory and upload it to the bootstrap staging bucket
// at deploy time. That upload is a PutObject against the S3 endpoint lstk injects
// via AWS_ENDPOINT_URL_S3, so a successful deploy proves the asset path routed
// through LocalStack. (Code.fromInline would embed the code in the template and
// never touch S3 — fromAsset is deliberate.)
const path = require("path");
const { App, Stack, CfnOutput } = require("aws-cdk-lib");
const { Function, Runtime, Code } = require("aws-cdk-lib/aws-lambda");

const app = new App();
const stack = new Stack(app, "LstkCdkLambdaE2eStack");
const fn = new Function(stack, "Handler", {
  functionName: "lstk-cdk-e2e-fn",
  runtime: Runtime.NODEJS_20_X,
  handler: "index.handler",
  code: Code.fromAsset(path.join(__dirname, "lambda")),
});
new CfnOutput(stack, "FunctionName", { value: fn.functionName });
app.synth();
