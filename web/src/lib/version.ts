// ESS chart version helpers.
//
// Kept out of the route component because this is logic, not view — and because
// it got the ordering wrong once already: a plain string sort ranked 26.5.1
// above 26.10.0 and hid every newer release from the upgrade picker.
// Mirrors compareVersions in internal/helm/versions.go.

/** Strips the chart-name prefix: "matrix-stack-26.5.1" → "26.5.1". */
export const essVersion = (v: string): string => v.replace(/^matrix-stack-/, "");

/**
 * Orders release tags numerically. Returns >0 if `a` is newer than `b`, <0 if
 * older, 0 if equal. A final release outranks its own prereleases.
 */
export function cmpVersion(a: string, b: string): number {
  const pa = a.replace(/^v/, "").split(/[.-]/);
  const pb = b.replace(/^v/, "").split(/[.-]/);
  for (let i = 0; i < 3; i++) {
    const na = parseInt(pa[i] ?? "0", 10) || 0;
    const nb = parseInt(pb[i] ?? "0", 10) || 0;
    if (na !== nb) return na - nb;
  }
  const preA = a.includes("-");
  const preB = b.includes("-");
  if (preA !== preB) return preA ? -1 : 1;
  return 0;
}
