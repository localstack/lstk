#!/bin/sh
# Stage of .spiral/check.sh: the integration suite (CI: Integration Tests), as
# far as a worker can run it. DOCKER_HOST points at a closed port, so every
# requireDocker test skips as on Windows CI: dockerd on a worker cannot pull
# images, and those tests need a LOCALSTACK_AUTH_TOKEN no worker holds.
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

# Tests that depend on the CI runner rather than lstk, so they run in CI only:
#   TestUpdateNotification                            the real api.github.com, closed on workers
#   TestUpdateRefusesWhenInstallDirIsNotWritable      directory permissions, which root bypasses
#   TestSetupAzureAliasAz,
#   TestSetupAzureNonInteractiveRunsWithoutTerminal   a real `az` on PATH, as GitHub runners have
skip='^(TestUpdateNotification|TestUpdateRefusesWhenInstallDirIsNotWritable|TestSetupAzureAliasAz|TestSetupAzureNonInteractiveRunsWithoutTerminal)$'

step "integration tests without Docker (skipping the runner-dependent tests listed in checks/integration.sh)"
make build
cd test/integration
DOCKER_HOST=tcp://localhost:1 go run gotest.tools/gotestsum@latest --format testname -- \
  -count=1 -timeout 25m -skip "$skip" ./...
