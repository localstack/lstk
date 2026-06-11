// Trivial handler whose code lives in a real file (not inline) so CDK zips and
// uploads it to the asset bucket. The e2e test invokes it and asserts this body.
exports.handler = async () => ({ statusCode: 200, body: "ok from lstk cdk lambda" });
