/** The real auth token, when the environment provides one (CI secret, or a local dev token). */
export function authToken(): string | undefined {
  return process.env.LOCALSTACK_AUTH_TOKEN || undefined;
}

/**
 * The auth token for tests that cannot run without one. Throws rather than
 * silently passing; callers guard with `describe.skipIf(!authToken())` so the
 * suite still runs for contributors without a token.
 */
export function requireAuthToken(): string {
  const token = authToken();
  if (!token) throw new Error("LOCALSTACK_AUTH_TOKEN must be set to run this test");
  return token;
}
