/** requestID identifies one translation run; getRandomValues works in
 * the packaged app, unlike crypto.randomUUID's secure context. */
export function requestID(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(8));
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}
