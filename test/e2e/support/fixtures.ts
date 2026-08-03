/**
 * A socket path that cannot exist. Pointing DOCKER_HOST here makes `start` fail
 * fast at the runtime ping, right after the flags and config have been applied —
 * which lets tests assert on config handling and messaging without a daemon.
 */
export const unreachableDockerHost =
  process.platform === "win32"
    ? "npipe:////./pipe/nonexistent-lstk-test"
    : "unix:///nonexistent-lstk-test.sock";
