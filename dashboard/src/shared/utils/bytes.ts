/**
 * Formats a positive byte count with binary (IEC) units, e.g. `5368709120` becomes `"5 GiB"`.
 *
 * Shared by every place the console shows a provider-reported artifact size or a picked file
 * size, so the unit choice and rounding stay identical. It assumes a usable positive value;
 * callers that must render an explicit "unknown" state should guard a non-positive value first
 * (for example the OS image catalog shows an em dash when the provider reports no size).
 */
export function formatBytes(bytes: number): string {
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  const unitIndex = Math.min(
    Math.floor(Math.log(bytes) / Math.log(1024)),
    units.length - 1,
  );
  const value = bytes / 1024 ** unitIndex;
  const formatted = new Intl.NumberFormat(undefined, {
    maximumFractionDigits: value >= 10 || unitIndex === 0 ? 0 : 1,
  }).format(value);
  return `${formatted} ${units[unitIndex]}`;
}
