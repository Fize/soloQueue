export function isBackendReady({ externalGoInstance, backendStartTime }) {
  // A spawned child exists before its HTTP health check succeeds.
  return Boolean(externalGoInstance || backendStartTime !== null)
}
