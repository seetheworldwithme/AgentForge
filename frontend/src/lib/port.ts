// Resolves the core service's base URL in two modes:
//  - Production (Wails): the port is exposed via the PortBinder binding
//    injected on window.go.main.PortBinder.GetPort().
//  - Dev (vite): CORE_PORT env var configures the proxy in vite.config.ts;
//    in that case requests stay same-origin and no port is needed here.

let cached: number | null = null;

export async function getCorePort(): Promise<number> {
  if (cached !== null) return cached;
  // Production: Wails binding injected on window.go.
  const w = window as any;
  if (w.go?.main?.PortBinder?.GetPort) {
    cached = await w.go.main.PortBinder.GetPort();
    return cached!;
  }
  // Dev: read from the vite env (matches CORE_PORT used by the proxy).
  cached = Number(import.meta.env.CORE_PORT) || 0;
  return cached;
}

export async function baseUrl(): Promise<string> {
  const port = await getCorePort();
  // In dev with the vite proxy, requests go through the same origin and the
  // proxy routes them to the running core; no port prefix is needed.
  if (!port) return '';
  return `http://127.0.0.1:${port}`;
}
