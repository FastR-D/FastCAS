/** Browser helpers never hold provider credentials or upstream refresh tokens. */
export function loginURL(path = "/api/auth/fastcas/login", returnTo = "/"): string {
  if (!path.startsWith("/") || path.startsWith("//") || /[\\\r\n]/.test(path) || !returnTo.startsWith("/") || returnTo.startsWith("//") || /[\\\r\n]/.test(returnTo)) throw new Error("Application-relative paths required");
  const separator = path.includes("?") ? "&" : "?";
  return `${path}${separator}returnTo=${encodeURIComponent(returnTo)}`;
}
export async function accountStatus<T>(path = "/api/auth/fastcas/status", signal?: AbortSignal): Promise<T> {
  if (!path.startsWith("/") || path.startsWith("//") || /[\\\r\n]/.test(path)) throw new Error("Application-relative path required");
  const response = await fetch(path, { credentials: "same-origin", signal, headers: { accept: "application/json" } });
  if (!response.ok) throw new Error("Unable to load account connection status");
  return response.json() as Promise<T>;
}
